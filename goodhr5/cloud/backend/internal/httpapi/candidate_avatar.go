// 本文件校验并规范化简历头像，只接收小型图片数据，不访问远程地址，不产生公开图片文件。
package httpapi

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"strings"
)

const candidateAvatarInputLimit = 1 << 20
const candidateAvatarOutputLimit = 128 << 10
const candidateAvatarMaxDimension = 4096
const candidateAvatarMaxPixels = 8_000_000
const candidateAvatarTargetSide = 256

// normalizeCandidateAvatar 校验真实图片并重编码小头像，空值表示保留原头像，不允许远程地址和 SVG。
func normalizeCandidateAvatar(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", errors.New("头像请使用图片数据")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	if len(text) > base64.StdEncoding.EncodedLen(candidateAvatarInputLimit)+40 {
		return "", errors.New("头像有点大，请选1MiB以内的图片")
	}
	header, payload, found := strings.Cut(text, ",")
	formats := map[string]string{"data:image/png;base64": "png", "data:image/jpeg;base64": "jpeg", "data:image/gif;base64": "gif"}
	expected, allowed := formats[header]
	if !found || !allowed || strings.ContainsAny(payload, "\r\n\t ") {
		return "", errors.New("头像只接受 PNG、JPEG 或 GIF 图片数据")
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(payload)
	if err != nil || len(raw) == 0 {
		return "", errors.New("头像内容没有读完整，请换一张图片")
	}
	if len(raw) > candidateAvatarInputLimit {
		return "", errors.New("头像有点大，请选1MiB以内的图片")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || format != expected {
		return "", errors.New("头像内容与图片格式不一致，请换一张图片")
	}
	if !candidateAvatarDimensionsAllowed(config.Width, config.Height) {
		return "", errors.New("头像像素太大，请缩小后再试")
	}
	decoded, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil || format != expected {
		return "", errors.New("头像没有解码成功，请换一张图片")
	}
	bounds := decoded.Bounds()
	if !candidateAvatarDimensionsAllowed(bounds.Dx(), bounds.Dy()) {
		return "", errors.New("头像像素太大，请缩小后再试")
	}
	// 重新编码只保留像素；GIF 取首帧，PNG/GIF 保留透明度，JPEG 去除原始元信息。
	for side := candidateAvatarTargetSide; side >= 1; side /= 2 {
		small := resizeCandidateAvatar(decoded, side)
		var output bytes.Buffer
		mime := "image/png"
		if format == "jpeg" {
			mime = "image/jpeg"
			err = jpeg.Encode(&output, small, &jpeg.Options{Quality: 85})
		} else {
			err = png.Encode(&output, small)
		}
		if err != nil {
			return "", errors.New("头像整理暂时没完成，请换一张图片")
		}
		prefix := "data:" + mime + ";base64,"
		if len(prefix)+base64.StdEncoding.EncodedLen(output.Len()) <= candidateAvatarOutputLimit {
			return prefix + base64.StdEncoding.EncodeToString(output.Bytes()), nil
		}
	}
	return "", errors.New("头像整理后还是太大，请换一张图片")
}

// candidateAvatarDimensionsAllowed 在实际解码前限制尺寸和像素总数，防止压缩小图占用大量内存。
func candidateAvatarDimensionsAllowed(width, height int) bool {
	return width > 0 && height > 0 && width <= candidateAvatarMaxDimension && height <= candidateAvatarMaxDimension && int64(width)*int64(height) <= candidateAvatarMaxPixels
}

// resizeCandidateAvatar 等比缩小头像且不放大，使用双线性采样保留边缘和透明度。
func resizeCandidateAvatar(source image.Image, side int) *image.RGBA {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width > side || height > side {
		scale := float64(side) / float64(max(width, height))
		width = max(1, int(math.Round(float64(width)*scale)))
		height = max(1, int(math.Round(float64(height)*scale)))
	}
	output := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		sy := max(0.0, min(float64(bounds.Dy()-1), (float64(y)+0.5)*float64(bounds.Dy())/float64(height)-0.5))
		y0 := int(sy)
		y1 := min(y0+1, bounds.Dy()-1)
		fy := sy - float64(y0)
		for x := 0; x < width; x++ {
			sx := max(0.0, min(float64(bounds.Dx()-1), (float64(x)+0.5)*float64(bounds.Dx())/float64(width)-0.5))
			x0 := int(sx)
			x1 := min(x0+1, bounds.Dx()-1)
			fx := sx - float64(x0)
			points := [4]image.Point{{x0, y0}, {x1, y0}, {x0, y1}, {x1, y1}}
			weights := [4]float64{(1 - fx) * (1 - fy), fx * (1 - fy), (1 - fx) * fy, fx * fy}
			var channels [4]float64
			for index, point := range points {
				r, g, b, a := source.At(bounds.Min.X+point.X, bounds.Min.Y+point.Y).RGBA()
				for channel, value := range [4]uint32{r, g, b, a} {
					channels[channel] += float64(value) * weights[index]
				}
			}
			output.SetRGBA(x, y, color.RGBA{R: uint8(math.Round(channels[0] / 257)), G: uint8(math.Round(channels[1] / 257)), B: uint8(math.Round(channels[2] / 257)), A: uint8(math.Round(channels[3] / 257))})
		}
	}
	return output
}
