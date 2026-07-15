// Package browser 文件作用：统一 GoodHR 自动化浏览器的固定内容视口尺寸。
package browser

const (
	// FixedViewportWidth 是自动化浏览器统一使用的 CSS 视口宽度。
	FixedViewportWidth = 1280
	// FixedViewportHeight 是自动化浏览器统一使用的 CSS 视口高度。
	FixedViewportHeight = 720
)

// FixedViewport 返回所有浏览器入口共用的固定内容视口尺寸。
func FixedViewport() (int, int) {
	return FixedViewportWidth, FixedViewportHeight
}
