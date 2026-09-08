package fbhttp

import (
	"fmt"
	"reflect"

	"github.com/filebrowser/filebrowser/v2/files"
)

// uploadObjectID returns a stable identity for an existing filesystem object.
// Dev+inode is intentionally used rather than size or mtime: both change as a
// valid resumable upload is appended to, while a delete-and-recreate obtains a
// different identity. Reflection keeps this package buildable with afero and on
// platforms whose stat structures differ; filesystems without both fields are
// rejected for TUS because they cannot safely support path rebinding.
func uploadObjectID(file *files.FileInfo) (string, error) {
	if file == nil {
		return "", fmt.Errorf("missing upload object")
	}
	info, err := file.Fs.Stat(file.Path)
	if err != nil {
		return "", err
	}
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return "", fmt.Errorf("filesystem does not expose a stable object identity")
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return "", fmt.Errorf("filesystem does not expose a stable object identity")
	}
	dev, ino := value.FieldByName("Dev"), value.FieldByName("Ino")
	if !dev.IsValid() || !ino.IsValid() || !dev.CanUint() || !ino.CanUint() {
		return "", fmt.Errorf("filesystem does not expose a stable object identity")
	}
	return fmt.Sprintf("%d:%d", dev.Uint(), ino.Uint()), nil
}
