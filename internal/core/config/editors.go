package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

type EditorSettings struct {
	Version      int               `json:"version"`
	Revision     uint64            `json:"revision"`
	Environments map[string]string `json:"environments"`
}

var ErrEditorRevision = errors.New("editor settings changed; refresh and retry")

func ReadEditorSettings(path string) (EditorSettings, error) {
	var out EditorSettings
	err := withWebLock(path, func() error {
		var err error
		out, err = readEditorSettings(path)
		return err
	})
	return out, err
}

func readEditorSettings(path string) (EditorSettings, error) {
	out := EditorSettings{Version: 1, Environments: map[string]string{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out = EditorSettings{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if out.Version != 1 || out.Environments == nil {
		return out, fmt.Errorf("invalid editor settings")
	}
	return out, nil
}

func UpdateEditorSettings(path, environment, editor string, expected uint64) (EditorSettings, error) {
	var out EditorSettings
	err := withWebLock(path, func() error {
		var err error
		out, err = readEditorSettings(path)
		if err != nil {
			return err
		}
		if out.Revision != expected {
			return ErrEditorRevision
		}
		if editor == "" {
			delete(out.Environments, environment)
		} else {
			out.Environments[environment] = editor
		}
		out.Revision++
		raw, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		return gitstore.WriteJSON(path, append(raw, '\n'))
	})
	return out, err
}
