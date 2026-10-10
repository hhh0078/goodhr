// Package resume_import 文件作用：构造真实OLE Word二进制，验证中文片段解码、加密拒绝和损坏文件安全退出。
package resume_import

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

// syntheticDOC 构造最小真实CFB容器及Word97 Unicode正文，文件只包含虚构履历。
func syntheticDOC(text string) []byte {
	data := make([]byte, 512*19)
	header := data[:512]
	copy(header, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1})
	put16 := func(offset int, value uint16) { binary.LittleEndian.PutUint16(header[offset:offset+2], value) }
	put32 := func(offset int, value uint32) { binary.LittleEndian.PutUint32(header[offset:offset+4], value) }
	put16(24, 0x003e)
	put16(26, 3)
	put16(28, 0xfffe)
	put16(30, 9)
	put16(32, 6)
	put32(44, 1)
	put32(48, 1)
	put32(56, 4096)
	put32(60, 0xfffffffe)
	put32(68, 0xfffffffe)
	for i := 76; i < 512; i += 4 {
		put32(i, 0xffffffff)
	}
	put32(76, 0)
	fat := data[512:1024]
	for i := 0; i < 128; i++ {
		binary.LittleEndian.PutUint32(fat[i*4:i*4+4], 0xffffffff)
	}
	for i := 0; i < 18; i++ {
		next := uint32(i + 1)
		if i == 0 {
			next = 0xfffffffd
		}
		if i == 1 || i == 9 || i == 17 {
			next = 0xfffffffe
		}
		binary.LittleEndian.PutUint32(fat[i*4:i*4+4], next)
	}
	dir := data[1024:1536]
	// writeEntry 填写CFB目录记录的UTF-16名称、树关系和真实流长度。
	writeEntry := func(index int, name string, kind byte, start, size uint32, right uint32) {
		entry := dir[index*128 : (index+1)*128]
		units := utf16.Encode([]rune(name + "\x00"))
		for i, u := range units {
			binary.LittleEndian.PutUint16(entry[i*2:i*2+2], u)
		}
		binary.LittleEndian.PutUint16(entry[64:66], uint16(len(units)*2))
		entry[66], entry[67] = kind, 1
		binary.LittleEndian.PutUint32(entry[68:72], 0xffffffff)
		binary.LittleEndian.PutUint32(entry[72:76], right)
		binary.LittleEndian.PutUint32(entry[76:80], 0xffffffff)
		binary.LittleEndian.PutUint32(entry[116:120], start)
		binary.LittleEndian.PutUint64(entry[120:128], uint64(size))
	}
	writeEntry(0, "Root Entry", 5, 0xfffffffe, 0, 0xffffffff)
	binary.LittleEndian.PutUint32(dir[76:80], 1)
	writeEntry(1, "WordDocument", 2, 2, 4096, 2)
	writeEntry(2, "0Table", 2, 10, 4096, 0xffffffff)
	word := data[1536 : 1536+4096]
	binary.LittleEndian.PutUint16(word[:2], 0xa5ec)
	binary.LittleEndian.PutUint16(word[2:4], 0x00c1)
	binary.LittleEndian.PutUint16(word[32:34], 14)
	binary.LittleEndian.PutUint16(word[62:64], 22)
	binary.LittleEndian.PutUint16(word[152:154], 93)
	units := utf16.Encode([]rune(text))
	for i, u := range units {
		binary.LittleEndian.PutUint16(word[1024+i*2:1024+i*2+2], u)
	}
	table := data[5632 : 5632+4096]
	table[0] = 2
	binary.LittleEndian.PutUint32(table[1:5], 16)
	binary.LittleEndian.PutUint32(table[9:13], uint32(len(units)))
	binary.LittleEndian.PutUint32(table[15:19], 1024)
	binary.LittleEndian.PutUint32(word[418:422], 0)
	binary.LittleEndian.PutUint32(word[422:426], 21)
	return data
}

// TestLegacyDOCChineseAndMalformed 验证真实DOC中文不重复、不乱码，非Word/加密/越界数据不崩溃。
func TestLegacyDOCChineseAndMalformed(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "合成履历.doc")
	expected := "张合成\r工作经历：虚构科技有限公司\r软件开发 2020-2025"
	if err := os.WriteFile(path, syntheticDOC(expected), 0600); err != nil {
		t.Fatal(err)
	}
	text, err := (&Extractor{Root: root}).Extract(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if text != strings.ReplaceAll(expected, "\r", "\n") {
		t.Fatalf("DOC正文=%q", text)
	}
	for _, data := range [][]byte{[]byte("not a doc"), make([]byte, 512), syntheticDOC(expected)} {
		if len(data) > 1536 {
			binary.LittleEndian.PutUint16(data[1536+10:1536+12], 0x0100)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := extractDOC(context.Background(), path); err == nil {
			t.Fatal("无效或加密DOC被接受")
		}
	}
	word := make([]byte, 512)
	binary.LittleEndian.PutUint16(word[32:34], 0xffff)
	if _, err := extractWordPieces(context.Background(), word, nil); err == nil {
		t.Fatal("越界FIB被接受")
	}
}
