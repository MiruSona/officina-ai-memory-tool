// Package fileio 는 남의·사용자 파일을 안전하게 덮어쓰는 도우미다.
//
// 바로 os.WriteFile 로 덮으면 쓰다 죽을 때 반쪽 파일이 남고, 읽은 뒤 남이
// 고친 것을 모르고 지운다. 여기서는 「읽어 둔 바이트와 같을 때만 · 옛 판을
// 한 벌 남기고 · 임시 파일에 다 쓴 뒤 rename 으로 한 번에」 바꾼다.
// 표준 라이브러리만 쓴다 (config 를 모른다).
package fileio

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrChanged 는 읽어 둔 뒤 파일이 바뀌어서 아무것도 안 바꿨다는 뜻이다.
// 부르는 쪽은 「다시 돌려라」 로 알린다.
var ErrChanged = errors.New("fileio: file changed since it was read")

// BackupSuffix 는 백업 파일 이름 끝이다. 파일마다 한 벌, 매번 덮는다.
const BackupSuffix = ".mem-bak"

// tempMark 는 임시 파일 이름에 붙는 표시다. `<path>.mem-tmp-<pid>-<난수>`.
const tempMark = ".mem-tmp-"

// renameTries · renameWaitCap 은 rename 이 막혔을 때 다시 해 보는 폭이다.
// Windows 는 남이 대상 파일을 잠깐 열고 있으면 덮기가 막힌다.
const renameTries = 6

const renameWaitCap = 20 * time.Millisecond

// ReplaceFile 은 path 의 지금 바이트가 before 와 같을 때만 data 로 바꾼다.
//   - path 가 없으면 before 가 비어 있을 때만 새로 만든다.
//   - 링크나 일반 파일이 아닌 것은 거절한다.
//   - before 와 backupDir 가 둘 다 있으면 backupDir/<이름>.mem-bak 에 옛
//     바이트를 먼저 남긴다.
//
// 백업·임시 파일 쓰기가 실패하면 원본은 그대로다. rename 이 끝내 실패하면
// 임시 파일만 지우고 백업은 남긴다.
func ReplaceFile(path string, data, before []byte, backupDir string) error {
	mode := os.FileMode(0o644)
	info, err := os.Lstat(path)
	switch {
	case os.IsNotExist(err):
		if len(before) > 0 {
			return ErrChanged
		}
	case err != nil:
		return err
	default:
		if !info.Mode().IsRegular() {
			return fmt.Errorf("fileio: not a regular file: %s", path)
		}
		mode = info.Mode().Perm()
		current, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, before) {
			return ErrChanged
		}
		if len(before) > 0 && backupDir != "" {
			if err := writeBackup(backupDir, filepath.Base(path), before, mode); err != nil {
				return err
			}
		}
	}
	tmp, err := writeTemp(path, data, mode)
	if err != nil {
		return err
	}
	if err := renameRetry(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// writeBackup 은 옛 바이트를 백업 폴더에 한 벌 남긴다. 폴더나 백업 파일이
// 링크면 거절한다 — 링크를 따라가 엉뚱한 파일을 덮으면 안 된다.
func writeBackup(dir, name string, old []byte, mode os.FileMode) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("fileio: backup dir is not a plain directory: %s", dir)
	}
	target := filepath.Join(dir, name+BackupSuffix)
	if info, err := os.Lstat(target); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("fileio: backup is not a regular file: %s", target)
	}
	tmp, err := writeTemp(target, old, mode)
	if err != nil {
		return err
	}
	if err := renameRetry(tmp, target); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// writeTemp 는 path 옆에 새 임시 파일을 만들어 data 를 다 쓰고 닫는다.
// O_EXCL 이라 남의 파일을 덮지 않는다.
func writeTemp(path string, data []byte, mode os.FileMode) (string, error) {
	var noise [6]byte
	if _, err := rand.Read(noise[:]); err != nil {
		return "", err
	}
	tmp := fmt.Sprintf("%s%s%d-%s", path, tempMark, os.Getpid(), hex.EncodeToString(noise[:]))
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return "", err
	}
	_, err = file.Write(data)
	if err == nil {
		// umask 에 깎이지 않게 원본 권한을 그대로 입힌다.
		err = file.Chmod(mode)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

// renameRetry 는 tmp 로 dst 를 덮는다. os.Rename 은 Windows 에서도 덮는다.
// 먼저 dst 를 지우면 rename 실패 때 옛 파일까지 잃으니 지우지 않는다.
func renameRetry(tmp, dst string) error {
	var err error
	wait := time.Millisecond
	for try := 0; try < renameTries; try++ {
		if err = os.Rename(tmp, dst); err == nil {
			return nil
		}
		if try == renameTries-1 {
			break
		}
		time.Sleep(wait)
		wait *= 2
		if wait > renameWaitCap {
			wait = renameWaitCap
		}
	}
	return err
}
