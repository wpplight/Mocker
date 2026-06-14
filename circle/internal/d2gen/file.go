// Package d2gen 文件写入辅助
package d2gen

import "os"

// writeFileImpl 默认实现：调 os.WriteFile
func writeFileImpl(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}
