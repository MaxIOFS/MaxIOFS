package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteFolderMarker_LeavesTheObjectsUnderItAlone(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-folder-delete-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	fs, err := NewFilesystemBackend(Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := fs.Put(ctx, "b1/photos/", strings.NewReader(""), nil); err != nil {
		t.Fatalf("could not create the folder marker: %v", err)
	}
	if err := fs.Put(ctx, "b1/photos/cat.jpg", strings.NewReader("the bytes"), nil); err != nil {
		t.Fatalf("could not write the object: %v", err)
	}

	if err := fs.Delete(ctx, "b1/photos/"); err != nil {
		t.Fatalf("deleting the folder marker failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "b1", "photos", "cat.jpg")); err != nil {
		t.Fatalf("deleting the folder marker destroyed the object under it: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "b1", "photos", ".maxiofs-folder")); !os.IsNotExist(err) {
		t.Fatal("the folder marker itself should be gone")
	}
}

func TestDeleteFolderMarker_RemovesAnEmptyFolder(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-folder-delete-empty-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	fs, err := NewFilesystemBackend(Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := fs.Put(ctx, "b1/empty/", strings.NewReader(""), nil); err != nil {
		t.Fatal(err)
	}
	if err := fs.Delete(ctx, "b1/empty/"); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, "b1", "empty")); !os.IsNotExist(err) {
		t.Fatal("an emptied folder should not be left behind")
	}
}
