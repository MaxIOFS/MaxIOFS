package storage

import (
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func openObjectFile(path string) (*os.File, error) {
	native, err := nativeObjectPath(path)
	if err != nil {
		return nil, err
	}
	p, err := windows.UTF16PtrFromString(native)
	if err != nil {
		return nil, err
	}
	// Keep this generation readable while a writer replaces or deletes its name.
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

func replaceObjectFile(oldPath, newPath string) error {
	if err := os.Rename(oldPath, newPath); err == nil {
		return nil
	}
	native, err := nativeObjectPath(oldPath)
	if err != nil {
		return err
	}
	old, err := windows.UTF16PtrFromString(native)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(old, windows.DELETE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return &os.PathError{Op: "open", Path: oldPath, Err: err}
	}
	defer windows.CloseHandle(h)
	absolute, err := nativeObjectPath(newPath)
	if err != nil {
		return err
	}
	name, err := windows.UTF16FromString(absolute)
	if err != nil {
		return err
	}
	type renameInfo struct {
		Flags          uint32
		RootDirectory  windows.Handle
		FileNameLength uint32
		FileName       [1]uint16
	}
	var layout renameInfo
	name = name[:len(name)-1]
	size := int(unsafe.Offsetof(layout.FileName)) + (len(name)+1)*2
	buffer := make([]byte, size)
	info := (*renameInfo)(unsafe.Pointer(&buffer[0]))
	info.Flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS
	info.FileNameLength = uint32(len(name) * 2)
	copy(unsafe.Slice(&info.FileName[0], len(name)), name)
	err = windows.SetFileInformationByHandle(h, windows.FileRenameInfoEx, &buffer[0], uint32(size))
	if err != nil {
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: err}
	}
	return nil
}

func nativeObjectPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(absolute, "\\\\?\\") {
		return absolute, nil
	}
	if strings.HasPrefix(absolute, "\\\\") {
		return "\\\\?\\UNC\\" + absolute[2:], nil
	}
	return "\\\\?\\" + absolute, nil
}
