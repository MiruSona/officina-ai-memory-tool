package embed

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
)

// Write 는 표를 우리 꼴로 쓴다. 진짜 표는 파이썬이 굽지만(`tools/embed`),
// 시험이 파이썬 없이 표를 만들어 볼 수 있어야 한다. 굽는 쪽과 읽는 쪽이
// 어긋나면 여기서 먼저 깨진다.
func Write(out io.Writer, pieces []string, weights []float32, vectors [][]float64) error {
	if len(pieces) == 0 || len(pieces) != len(weights) || len(pieces) != len(vectors) {
		return errors.New("어휘·가중치·벡터 수가 안 맞는다")
	}
	dim := len(vectors[0])
	if dim <= 0 || dim > MaxDim {
		return errors.New("차원이 이상하다")
	}
	biggest := 0.0
	for _, vector := range vectors {
		if len(vector) != dim {
			return errors.New("벡터 길이가 제각각이다")
		}
		for _, value := range vector {
			if math.Abs(value) > biggest {
				biggest = math.Abs(value)
			}
		}
	}
	scale := biggest / 127
	if scale <= 0 {
		scale = 1
	}
	head := make([]byte, 0, len(Magic)+16)
	head = append(head, Magic...)
	head = binary.LittleEndian.AppendUint32(head, Version)
	head = binary.LittleEndian.AppendUint32(head, uint32(dim))
	head = binary.LittleEndian.AppendUint32(head, uint32(len(pieces)))
	head = binary.LittleEndian.AppendUint32(head, math.Float32bits(float32(scale)))
	if _, err := out.Write(head); err != nil {
		return err
	}
	for _, piece := range pieces {
		raw := []byte(piece)
		if len(raw) > 255 {
			return errors.New("조각이 255바이트를 넘는다")
		}
		if _, err := out.Write(append([]byte{byte(len(raw))}, raw...)); err != nil {
			return err
		}
	}
	line := make([]byte, 0, 4*len(weights))
	for _, weight := range weights {
		line = binary.LittleEndian.AppendUint32(line, math.Float32bits(weight))
	}
	if _, err := out.Write(line); err != nil {
		return err
	}
	body := make([]byte, 0, len(pieces)*dim)
	for _, vector := range vectors {
		for _, value := range vector {
			body = append(body, byte(int8(math.Round(math.Max(-127, math.Min(127, value/scale))))))
		}
	}
	_, err := out.Write(body)
	return err
}
