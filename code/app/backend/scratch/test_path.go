package main
import (
	"fmt"
	"path/filepath"
)
func main() {
	staticPath := "./static.min"
	urlPath := "/assets/index-D7MtlJJD.js"
	fpath := filepath.Join(staticPath, filepath.Clean(urlPath))
	fmt.Printf("staticPath: %s\n", staticPath)
	fmt.Printf("urlPath: %s\n", urlPath)
	fmt.Printf("fpath: %s\n", fpath)
}
