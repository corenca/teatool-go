// Package teatool 是工具库的占位包，实际功能在各自子包中：batch、collection、
// datetime、filesplit、fsutil、masking、gm、storage。引入本模块时不需要使用它。
package teatool

import (
	"fmt"
)

// Hello 打印一行问候语，只用于验证依赖引入与构建链路是否正常。
func Hello() {
	fmt.Println("Hello Teatool")
}
