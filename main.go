package main

import (
	"fmt"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"
)

const cloneDir = "eqemupatcher" // Directory to clone the repository to

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
	e.GET("/gh-update", func(c echo.Context) error {
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
		cmd := exec.Command("git", "clone", os.Getenv("REPO_URL"), "eqemupatcher")
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
