package agent

import (
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// decoderForCodePage 把 Windows 代码页映射到 x/text 解码器；返回 nil 表示
// 无需转码（UTF-8 直通或未知代码页保持既有行为）。子进程经管道输出文本时
// 用的不是 UTF-8，而是控制台代码页（cmd 与 PowerShell 5.1 均为 OEM CP）：
// 中文系统是 936，不转码的话 scanner 按 UTF-8 逐字节解码会把整行输出变成
// U+FFFD 替换符（issue #5 Bug 1）。
func decoderForCodePage(cp uint) encoding.Encoding {
	switch cp {
	case 0, 65001: // UTF-8
		return nil
	case 936: // 简体中文；GB18030 是 cp936 的超集，向下兼容解码
		return simplifiedchinese.GB18030
	case 950: // 繁体中文
		return traditionalchinese.Big5
	case 932: // 日语
		return japanese.ShiftJIS
	case 949: // 韩语
		return korean.EUCKR
	case 437: // 美式 OEM
		return charmap.CodePage437
	case 850: // 拉丁-1 OEM
		return charmap.CodePage850
	case 852: // 中欧 OEM
		return charmap.CodePage852
	case 866: // 西里尔 OEM
		return charmap.CodePage866
	default: // 未知代码页：直通，不引入错误的转码
		return nil
	}
}
