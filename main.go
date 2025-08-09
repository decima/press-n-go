package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/securecookie"
	"github.com/spf13/viper"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// --- Structs ---

type Config struct {
	Username string `mapstructure:"PNG_USERNAME"`
	Password string `mapstructure:"PNG_PASSWORD"`
}

type UploadRequest struct {
	Content  string `json:"content"   binding:"required"`
	Type     string `json:"type"      binding:"required"`
	ThemeCSS string `json:"themeCSS"`
}

type Page struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
}

// --- Global Variables ---

var (
	appConfig     Config
	md            goldmark.Markdown
	cookieHandler *securecookie.SecureCookie
)

// --- Initialization ---

func init() {
	// Initialize Goldmark Markdown converter
	md = goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(html.WithHardWraps(), html.WithUnsafe()),
	)

	// Initialize secure cookie handler
	hashKey := securecookie.GenerateRandomKey(64)
	blockKey := securecookie.GenerateRandomKey(32)
	cookieHandler = securecookie.New(hashKey, blockKey)
}

func main() {
	// Load configuration
	LoadConfig()

	// Ensure 'public' directory exists
	if _, err := os.Stat("public"); os.IsNotExist(err) {
		os.Mkdir("public", 0755)
	}

	// Setup Gin router
	router := gin.Default()
	router.LoadHTMLGlob("templates/*.html")

	// serve assets folder on /assets
	router.StaticFS("/assets", http.Dir("assets"))

	// Use the static middleware to serve generated pages from the root.
	router.Use(static.Serve("/", static.LocalFile("./public", false)))

	// Login/Logout routes are public
	router.GET("/login", showLoginPage)
	router.POST("/login", handleLogin)
	router.GET("/logout", handleLogout) // New logout route

	// Publisher panel is now at the root URL with custom auth
	publishGroup := router.Group("/")
	publishGroup.Use(authRequired())
	{
		publishGroup.GET("/", func(c *gin.Context) {
			c.HTML(http.StatusOK, "index.html", nil)
		})
	}

	// API routes with custom auth
	api := router.Group("/api")
	api.Use(authRequired())
	{
		api.POST("/upload", handleUpload)
		api.GET("/pages", handleListPages)
		api.DELETE("/pages/:id", handleDeletePage)
		api.GET("/pages/:id/source", handleDownloadSource)
	}

	// Add a handler for 404 Not Found errors
	router.NoRoute(func(c *gin.Context) {
		c.HTML(http.StatusNotFound, "404.html", nil)
	})

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server starting on http://localhost:%s", port)
	log.Printf("Publishing interface available at http://localhost:%s/", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}

// --- Custom Middleware ---

func isAuthenticated(c *gin.Context) bool {
	cookie, err := c.Cookie("session")
	if err != nil {
		return false
	}

	cookieValue := make(map[string]string)
	if err = cookieHandler.Decode("session", cookie, &cookieValue); err != nil {
		return false
	}

	return cookieValue["authenticated"] == "true"
}

// --- Middleware ---
func authRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		if appConfig.Username == "" || appConfig.Password == "" || isAuthenticated(c) {
			c.Next()
			return
		}
		c.Redirect(http.StatusFound, "/login")
		c.Abort()
	}
}

// --- Handlers ---

func showLoginPage(c *gin.Context) {
	c.HTML(http.StatusOK, "login.html", nil)
}

func createSession(c *gin.Context) error {
	value := map[string]string{"authenticated": "true"}
	encoded, err := cookieHandler.Encode("session", value)
	if err != nil {
		return err
	}
	c.SetCookie("session", encoded, 3600*24, "/", "", false, true)
	return nil
}

func handleLogin(c *gin.Context) {
	username, password := c.PostForm("username"), c.PostForm("password")
	if username == appConfig.Username && password == appConfig.Password {
		if err := createSession(c); err != nil {
			c.HTML(http.StatusInternalServerError, "login.html", gin.H{"Error": "Failed to create session"})
			return
		}
		c.Redirect(http.StatusFound, "/")
	} else {
		c.HTML(http.StatusUnauthorized, "login.html", gin.H{"Error": "Invalid username or password"})
	}
}

func handleLogout(c *gin.Context) {
	// Set the cookie with a max age of -1 to delete it
	c.SetCookie("session", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/login")
}

func generatePageID() (string, error) {
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random ID: %w", err)
	}
	return hex.EncodeToString(randomBytes), nil
}

func handleUpload(c *gin.Context) {
	var req UploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	pageID, err := generatePageID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := createPageFile(pageID, req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"url": fmt.Sprintf("/%s/", pageID)})
}

func handleListPages(c *gin.Context) {
	var discoveredPages []Page
	entries, err := os.ReadDir("public")
	if err != nil {
		log.Printf("Error reading public directory: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not list pages"})
		return
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "index.html" {
			info, err := entry.Info()
			if err != nil {
				log.Printf("Error getting info for %s: %v", entry.Name(), err)
				continue
			}
			discoveredPages = append(discoveredPages, Page{
				ID:        entry.Name(),
				CreatedAt: info.ModTime(),
			})
		}
	}
	c.JSON(http.StatusOK, discoveredPages)
}

func handleDeletePage(c *gin.Context) {
	pageID := c.Param("id")
	if pageID == "" || strings.Contains(pageID, ".") || strings.Contains(pageID, "/") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page ID"})
		return
	}
	folderPath := filepath.Join("public", pageID)
	if _, err := os.Stat(folderPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Page not found"})
		return
	}
	if err := os.RemoveAll(folderPath); err != nil {
		log.Printf("Error deleting folder %s: %v", folderPath, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete page"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Page deleted successfully"})
}

func handleDownloadSource(c *gin.Context) {
	pageID := c.Param("id")
	if pageID == "" || strings.Contains(pageID, ".") || strings.Contains(pageID, "/") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page ID"})
		return
	}
	sourcePath := filepath.Join("public", pageID, "source.txt")
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Source file not found"})
		return
	}
	c.FileAttachment(sourcePath, fmt.Sprintf("%s_source.txt", pageID))
}

// --- Helper Functions ---

func LoadConfig() {
	viper.SetDefault("PNG_USERNAME", "")
	viper.SetDefault("PNG_PASSWORD", "")
	viper.AutomaticEnv()
	if err := viper.Unmarshal(&appConfig); err != nil {
		log.Fatalf("Unable to decode config into struct, %v", err)
	}
}

func createPageFile(pageID string, req UploadRequest) error {
	folderPath := filepath.Join("public", pageID)
	if err := os.MkdirAll(folderPath, 0755); err != nil {
		return fmt.Errorf("failed to create content directory: %w", err)
	}
	rawFilePath := filepath.Join(folderPath, "source.txt")
	if err := os.WriteFile(rawFilePath, []byte(req.Content), 0644); err != nil {
		return fmt.Errorf("failed to write raw source file: %w", err)
	}
	var finalContent string
	if req.Type == "markdown" {
		var buf bytes.Buffer
		if err := md.Convert([]byte(req.Content), &buf); err != nil {
			return fmt.Errorf("failed to convert markdown: %w", err)
		}
		htmlContent := buf.String()
		finalContent = fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Published Content</title>
    <style>%s</style>
</head>
<body><article class="markdown-body">%s</article></body>
</html>`, req.ThemeCSS, htmlContent)
	} else {
		finalContent = req.Content
	}
	filePath := filepath.Join(folderPath, "index.html")
	if err := os.WriteFile(filePath, []byte(finalContent), 0644); err != nil {
		return fmt.Errorf("failed to write rendered html file: %w", err)
	}
	return nil
}
