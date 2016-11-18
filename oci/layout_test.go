package oci

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/net/context"
)

func TestOpen(t *testing.T) {
	ctx := context.Background()

	root, err := ioutil.TempDir("", "oci-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	image := filepath.Join(root, "busybox")

	layout, err := Open(image)
	if err != nil {
		t.Fatalf("unexpected error opening image: %s", err)
	}
	defer layout.Close()

	// We should have no references or blobs.
	if refs, err := layout.ListReferences(ctx); err != nil {
		t.Errorf("unexpected error getting list of references: %s", err)
	} else if len(refs) > 0 {
		t.Errorf("got references in a newly created image: %v", refs)
	}
	if blobs, err := layout.ListBlobs(ctx); err != nil {
		t.Errorf("unexpected error getting list of blobs: %s", err)
	} else if len(blobs) > 0 {
		t.Errorf("got blobs in a newly created image: %v", blobs)
	}
}
