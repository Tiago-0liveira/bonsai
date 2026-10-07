//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package agentterminal

import (
	"fmt"
	"os"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const Supported = false

type process struct{ file *os.File }

func launch(agents.PreparedSession, int, int) (*process, error) {
	return nil, fmt.Errorf("interactive terminals unsupported on this platform")
}
func (p *process) resize(int, int) error { return fmt.Errorf("unsupported") }
func (p *process) signal(bool)           {}
func (p *process) wait() (int, error)    { return -1, fmt.Errorf("unsupported") }
