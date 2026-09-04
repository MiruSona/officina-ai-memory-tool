package index

import "errors"

// secretError 는 「비밀정보라 막았다」는 표다. 다른 실패(규격 위반·파일 오류)와
// 섞이면 `mem index` 가 종료 코드 2(검사 실패)와 4(보안 차단)를 못 가른다
// (보안연동 시험 M-2 · 설계 8절 종료 코드 표).
type secretError struct{ err error }

func (e *secretError) Error() string { return e.err.Error() }

func (e *secretError) Unwrap() error { return e.err }

// blocked 는 비밀정보 오류에 표를 단다.
func blocked(err error) error {
	if err == nil {
		return nil
	}
	return &secretError{err: err}
}

// IsSecretBlock 은 그 오류가 비밀정보 차단인지 말한다.
func IsSecretBlock(err error) bool {
	target := &secretError{}
	return errors.As(err, &target)
}
