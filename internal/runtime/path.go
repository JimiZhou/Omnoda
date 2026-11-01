package runtime

import (
	"os"
	"path/filepath"
)

func filepathAbsEval(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

// SafeJoin resolves rel inside root and rejects escapes, including symlink targets.
func SafeJoin(root, rel string) (string, error) {
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) {
		return "", policyErr()
	}
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) {
		return "", policyErr()
	}
	full := filepath.Join(root, clean)
	if relPath, err := filepath.Rel(root, full); err != nil || len(relPath) >= 2 && relPath[0] == '.' && relPath[1] == '.' {
		return "", policyErr()
	}
	if fi, err := os.Lstat(full); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		target, err := filepath.EvalSymlinks(full)
		if err != nil {
			return "", err
		}
		if relPath, err := filepath.Rel(root, target); err != nil || len(relPath) >= 2 && relPath[0] == '.' && relPath[1] == '.' {
			return "", policyErr()
		}
		return target, nil
	}
	return full, nil
}

type pathError string

func (e pathError) Error() string { return string(e) }

func policyErr() error { return pathError("path escapes workspace") }
