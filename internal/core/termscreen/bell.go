package termscreen

import (
	"io"
	"os"
)

// Bell emits the terminal attention signal without changing the screen.
func Bell() error {
	_, err := io.WriteString(os.Stdout, "\a")
	return err
}
