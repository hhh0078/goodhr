// Package resume_import 文件作用：有界读取旧 Word OLE 文档的 FIB、CLX 和文字片段，不执行宏、外链或 Office 程序。
package resume_import

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf16"

	"github.com/richardlehane/mscfb"
	"golang.org/x/text/encoding/charmap"
)

// extractDOC 只接受真实 OLE Word 文档，第三方容器解析异常转换为可恢复的错误。
func extractDOC(ctx context.Context, path string) (text string, err error) {
	defer func() {
		if recover() != nil {
			text, err = "", fmt.Errorf("Word文件结构损坏，请重新导出DOCX或PDF")
		}
	}()
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() < 512 || info.Size() > MaxFileBytes {
		return "", fmt.Errorf("Word文件为空或超过10MB")
	}
	header := make([]byte, 512)
	if _, err = file.ReadAt(header, 0); err != nil || !bytes.Equal(header[:8], []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) {
		return "", fmt.Errorf("这不是旧Word二进制文件，请重新导出DOCX或PDF")
	}
	shift := binary.LittleEndian.Uint16(header[30:32])
	if shift != 9 && shift != 12 {
		return "", fmt.Errorf("Word容器扇区格式不正确")
	}
	maxSectors := uint32(info.Size() / (1 << shift))
	for _, offset := range []int{40, 44, 64, 72} {
		if binary.LittleEndian.Uint32(header[offset:offset+4]) > maxSectors {
			return "", fmt.Errorf("Word容器声明大小超过实际文件")
		}
	}
	container, err := mscfb.New(docContextReader{ctx, file})
	if err != nil {
		return "", fmt.Errorf("Word容器没读出来，请重新导出DOCX或PDF")
	}
	streams := map[string][]byte{}
	for _, stream := range container.File {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if stream.Name != "WordDocument" && stream.Name != "0Table" && stream.Name != "1Table" {
			continue
		}
		if stream.Size < 0 || stream.Size > MaxFileBytes {
			return "", fmt.Errorf("Word正文过大，请分开导入")
		}
		data, readErr := io.ReadAll(io.LimitReader(stream, MaxFileBytes+1))
		if readErr != nil || len(data) > MaxFileBytes {
			return "", fmt.Errorf("Word正文没读完整")
		}
		streams[stream.Name] = data
	}
	word := streams["WordDocument"]
	if len(word) < 34 || binary.LittleEndian.Uint16(word[:2]) != 0xa5ec {
		return "", fmt.Errorf("没找到Word正文")
	}
	flags := binary.LittleEndian.Uint16(word[10:12])
	if flags&0x0100 != 0 || flags&0x8000 != 0 {
		return "", fmt.Errorf("Word文档已加密，请解密后导入")
	}
	table := streams["0Table"]
	if flags&0x0200 != 0 {
		table = streams["1Table"]
	}
	return extractWordPieces(ctx, word, table)
}

// docContextReader 让第三方容器每次读取都能响应取消和五分钟提取超时。
type docContextReader struct {
	ctx    context.Context
	reader io.ReaderAt
}

// ReadAt 取消后拒绝读取，避免损坏文件长时间占用解析槽位。
func (r docContextReader) ReadAt(data []byte, offset int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.ReadAt(data, offset)
}

// extractWordPieces 按 MS-DOC 2.4.1 解码文字片段，逐次校验偏移、数量和总长度。
func extractWordPieces(ctx context.Context, word, table []byte) (string, error) {
	invalid := fmt.Errorf("Word文字索引不完整，请重新导出DOCX或PDF")
	if len(word) < 34 {
		return "", invalid
	}
	pos := 34 + int(binary.LittleEndian.Uint16(word[32:34]))*2
	if pos+2 > len(word) {
		return "", invalid
	}
	pos += 2 + int(binary.LittleEndian.Uint16(word[pos:pos+2]))*4
	if pos+2 > len(word) || binary.LittleEndian.Uint16(word[pos:pos+2]) < 34 {
		return "", invalid
	}
	pos += 2 + 33*8
	if pos+8 > len(word) {
		return "", invalid
	}
	start, size := int(binary.LittleEndian.Uint32(word[pos:pos+4])), int(binary.LittleEndian.Uint32(word[pos+4:pos+8]))
	if start < 0 || size < 5 || start > len(table) || size > len(table)-start {
		return "", invalid
	}
	clx := table[start : start+size]
	for len(clx) > 0 && clx[0] == 1 {
		if len(clx) < 3 {
			return "", invalid
		}
		n := 3 + int(binary.LittleEndian.Uint16(clx[1:3]))
		if n > len(clx) {
			return "", invalid
		}
		clx = clx[n:]
	}
	if len(clx) < 5 || clx[0] != 2 {
		return "", invalid
	}
	n := int(binary.LittleEndian.Uint32(clx[1:5]))
	if n < 4 || n > len(clx)-5 || (n-4)%12 != 0 {
		return "", invalid
	}
	pieces := clx[5 : 5+n]
	count := (n - 4) / 12
	if count > 20000 {
		return "", invalid
	}
	var output strings.Builder
	for i := 0; i < count; i++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		cp, next := int(binary.LittleEndian.Uint32(pieces[i*4:i*4+4])), int(binary.LittleEndian.Uint32(pieces[(i+1)*4:(i+1)*4+4]))
		if next < cp || next-cp > 200000 || output.Len() > 2<<20 {
			return "", invalid
		}
		pcd := pieces[(count+1)*4+i*8 : (count+1)*4+(i+1)*8]
		fc := binary.LittleEndian.Uint32(pcd[2:6])
		compressed := fc&0x40000000 != 0
		offset := int(fc & 0x3fffffff)
		length := next - cp
		if compressed {
			offset /= 2
		} else {
			length *= 2
		}
		if offset < 0 || offset > len(word) || length > len(word)-offset {
			return "", invalid
		}
		data := word[offset : offset+length]
		if compressed {
			decoded, err := charmap.Windows1252.NewDecoder().Bytes(data)
			if err != nil {
				return "", invalid
			}
			output.Write(decoded)
		} else {
			units := make([]uint16, len(data)/2)
			for j := range units {
				units[j] = binary.LittleEndian.Uint16(data[j*2 : j*2+2])
			}
			output.WriteString(string(utf16.Decode(units)))
		}
	}
	// 域指令是格式数据，仅保留展示内容；表格单元格和段落均转成正常分隔。
	var clean strings.Builder
	instruction := false
	for _, r := range output.String() {
		switch r {
		case 0x13:
			instruction = true
		case 0x14, 0x15:
			instruction = false
		case '\r', 0x07:
			if !instruction {
				clean.WriteByte('\n')
			}
		default:
			if !instruction && (r == '\n' || r == '\t' || r >= 32) {
				clean.WriteRune(r)
			}
		}
	}
	return strings.TrimSpace(clean.String()), nil
}
