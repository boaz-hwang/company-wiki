// Command company-wiki maintains an LLM wiki of the knowledge an organization has adopted.
package main

import (
	"os"

	"github.com/boaz-hwang/company-wiki/internal/wiki"
)

func main() {
	os.Exit(wiki.Main(os.Args[1:], wiki.OSIO()))
}
