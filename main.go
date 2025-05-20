package main

import (
	"archive/zip"
	"fmt"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const cloneDir = "eqemupatcher" // Directory to clone the repository to
const tempZipDir = "/tmp/patcher"

func main() {
	// load .env
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	cloneOrPull()

	e := echo.New()
	e.Use(middleware.Logger())

	// Webhook endpoint to trigger the pull or clone
	e.POST("/gh-update", func(c echo.Context) error {
		// Retrieve the secret key from the query string
		queryKey := c.QueryParam("key")
		expectedKey := os.Getenv("WEBHOOK_KEY")

		if queryKey == "" || queryKey != expectedKey {
			return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Invalid or missing key."})
		}

		go func() {
			time.Sleep(5 * time.Second)
			cloneOrPull()
		}()

		return c.JSON(http.StatusOK, echo.Map{"message": "Update triggered."})
	})

	var (
		chunkStore   = make(map[string][]string) // chunkID -> file list
		chunkStoreMu sync.Mutex
	)

	// POST /zip-chunks/init
	e.POST("/zip-chunks/init", func(c echo.Context) error {
		var payload struct {
			Files        []string `json:"files"`
			MaxChunkSize int64    `json:"max_chunk_size"` // bytes
		}
		if err := c.Bind(&payload); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid JSON payload")
		}

		// Default to 10MB if not provided
		if payload.MaxChunkSize <= 0 {
			payload.MaxChunkSize = 30 * 1024 * 1024 // 30MB
		}

		// Expand file paths with size data
		var filesWithSize []struct {
			Path string
			Size int64
		}
		for _, file := range payload.Files {
			full := filepath.Join(cloneDir, file)
			info, err := os.Stat(full)
			if err != nil || info.IsDir() {
				continue // skip if missing or directory
			}
			filesWithSize = append(filesWithSize, struct {
				Path string
				Size int64
			}{file, info.Size()})
		}

		// Chunk files by max total byte size
		chunks := chunkBySize(filesWithSize, payload.MaxChunkSize)

		// Store chunks using unique ID
		chunkID := strconv.FormatInt(time.Now().UnixNano(), 10)
		chunkStoreMu.Lock()
		for i, chunk := range chunks {
			var names []string
			for _, f := range chunk {
				names = append(names, f.Path)
			}
			chunkStore[chunkID+"-"+strconv.Itoa(i)] = names
		}
		chunkStoreMu.Unlock()

		// Return chunk URLs
		var urls []string
		for i := range chunks {
			urls = append(urls, fmt.Sprintf("/zip-chunks/%s-%d", chunkID, i))
		}

		type ChunkInfo struct {
			URL                   string `json:"url"`
			FileCount             int    `json:"file_count"`
			TotalSizeUncompressed int64  `json:"total_size_uncompressed"` // uncompressed size in bytes
		}

		var result []ChunkInfo

		for i, chunk := range chunks {
			var size int64
			for _, f := range chunk {
				size += f.Size
			}

			result = append(result, ChunkInfo{
				URL:                   fmt.Sprintf("/zip-chunks/%s-%d", chunkID, i),
				FileCount:             len(chunk),
				TotalSizeUncompressed: size,
			})
		}

		return c.JSON(http.StatusOK, echo.Map{
			"chunks": result,
		})
	})

	// GET /zip-chunks/:chunkID
	e.GET("/zip-chunks/:chunkID", func(c echo.Context) error {
		chunkID := c.Param("chunkID")

		chunkStoreMu.Lock()
		files, ok := chunkStore[chunkID]
		chunkStoreMu.Unlock()
		if !ok {
			return echo.NewHTTPError(http.StatusNotFound, "Chunk not found")
		}

		// Ensure /tmp/patcher/ exists
		tmpDir := filepath.Join(os.TempDir(), "patcher")
		if err := os.MkdirAll(tmpDir, 0o755); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create temp dir")
		}

		// Create a temp file under /tmp/patcher/
		tmpFile, err := os.CreateTemp(tmpDir, chunkID+"-*.zip")
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create temp zip")
		}
		defer tmpFile.Close()

		zipWriter := zip.NewWriter(tmpFile)
		for _, f := range files {
			fullPath := filepath.Join(cloneDir, f)
			file, err := os.Open(fullPath)
			if err != nil {
				continue
			}
			defer file.Close()

			w, err := zipWriter.Create(f)
			if err != nil {
				continue
			}
			io.Copy(w, file)
		}
		zipWriter.Close()

		// Get full path of created zip
		tmpPath := tmpFile.Name()

		fmt.Printf("Downloading %s\n", filepath.Join(tmpDir, chunkID))

		// Use a custom stream that deletes the file 3 minutes after the download completes
		return c.Stream(http.StatusOK, "application/zip", &delayedDeleteFile{
			path:    tmpPath,
			chunkID: chunkID,
			delay:   3 * time.Minute,
			onDelete: func() {
				fmt.Printf("Deleting %s\n", filepath.Join(tmpDir, chunkID))
				chunkStoreMu.Lock()
				delete(chunkStore, chunkID)
				chunkStoreMu.Unlock()
			},
		})
	})

	// Serve the static files
	e.Use(middleware.StaticWithConfig(middleware.StaticConfig{
		Root:   cloneDir,
		Browse: true,
	}))

	e.Logger.Fatal(e.Start(fmt.Sprintf(":4444")))
}

// cloneOrPull clones the repository if it doesn't exist, or pulls the latest changes if it does
func cloneOrPull() {
	if _, err := os.Stat(cloneDir); os.IsNotExist(err) {
		// Directory doesn't exist, clone the repository
		fmt.Println("Directory does not exist. Cloning repository...")
		cmd := exec.Command("git", "clone", os.Getenv("REPO_URL"), cloneDir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		fmt.Println("Cloning repository...")

		err = cmd.Run()
		if err != nil {
			fmt.Printf("Error cloning repository: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Repository cloned successfully.")
	} else {
		cmd := exec.Command("git", "-C", cloneDir, "pull")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		fmt.Println("Cloning repository...")

		err = cmd.Run()
		if err != nil {
			fmt.Printf("Error pulling repository: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Repository updated successfully.")
	}
}

func chunkBySize(files []struct {
	Path string
	Size int64
}, maxSize int64) [][]struct {
	Path string
	Size int64
} {
	var chunks [][]struct {
		Path string
		Size int64
	}
	var current []struct {
		Path string
		Size int64
	}
	var currentSize int64

	for _, f := range files {
		if currentSize+f.Size > maxSize && len(current) > 0 {
			chunks = append(chunks, current)
			current = nil
			currentSize = 0
		}
		current = append(current, f)
		currentSize += f.Size
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

type delayedDeleteFile struct {
	path     string
	chunkID  string
	delay    time.Duration
	onDelete func()
}

func (d *delayedDeleteFile) Read(p []byte) (int, error) {
	return 0, io.EOF
}

func (d *delayedDeleteFile) WriteTo(w io.Writer) (int64, error) {
	f, err := os.Open(d.path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	n, err := io.Copy(w, f)

	// After streaming finishes, schedule deletion
	time.AfterFunc(d.delay, func() {
		_ = os.Remove(d.path)
		if d.onDelete != nil {
			d.onDelete()
		}
	})

	return n, err
}
