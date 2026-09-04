package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// 같은 씨앗이면 같은 말뭉치여야 한다 (설계 13-1).
func TestSameSeedSameCorpus(t *testing.T) {
	first := makeCorpus(t)
	second := makeCorpus(t)
	if first != second {
		t.Fatalf("같은 씨앗인데 다른 말뭉치가 나왔다 : %s vs %s", first, second)
	}
}

func makeCorpus(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "Memory")
	settings := options{count: 40, seed: 7, out: out, words: filepath.Join("..", "corpus-words.txt")}
	if err := build(settings); err != nil {
		t.Fatal(err)
	}
	return treeHash(t, out)
}

func treeHash(t *testing.T, root string) string {
	t.Helper()
	sum := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sum.Write([]byte(filepath.ToSlash(rel)))
		sum.Write(text)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(sum.Sum(nil))
}
