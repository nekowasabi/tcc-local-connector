package frontend

type LineFramer struct {
	buffer []byte
}

func (f *LineFramer) Push(data []byte) [][]byte {
	f.buffer = append(f.buffer, data...)
	var lines [][]byte
	for {
		index := indexByte(f.buffer, '\n')
		if index < 0 {
			break
		}
		line := append([]byte(nil), f.buffer[:index]...)
		f.buffer = f.buffer[index+1:]
		if len(line) <= FrontendMaxLineBytes {
			lines = append(lines, line)
		}
	}
	if len(f.buffer) > FrontendMaxLineBytes {
		f.buffer = f.buffer[:0]
	}
	return lines
}

func indexByte(data []byte, value byte) int {
	for i, b := range data {
		if b == value {
			return i
		}
	}
	return -1
}
