package store

import (
	"os"
	"strconv"
)

// itoa is a small test-local integer formatter.
func itoa(i int) string { return strconv.Itoa(i) }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
