//go:build windows

// Package sessionfile 文件作用：通过 Windows 当前用户 SID 的独占 DACL 保护本地会话，不依赖无效的 Unix 权限位。
package sessionfile

import "golang.org/x/sys/windows"

// protectPath 取消父目录权限继承，仅允许当前 Windows 用户访问；目录权限继续继承给其子文件。
func protectPath(path string, directory bool) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	inheritance := ""
	if directory {
		inheritance = "OICI"
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;" + inheritance + ";FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}
