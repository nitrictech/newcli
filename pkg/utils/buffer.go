package utils

import "io"

type LineBuffer struct {
	limit int
	Lines []string
}

func (lb *LineBuffer) Write(p []byte) (int, error) {

	if len(lb.Lines) >= lb.limit {
		lb.Lines = append(lb.Lines[1:], string(p))
	} else {
		lb.Lines = append(lb.Lines, string(p))
	}

	return len(p), nil
}

var _ io.Writer = &LineBuffer{}

func NewLineBuffer(limit int) *LineBuffer {
	return &LineBuffer{
		limit: limit,
		Lines: []string{},
	}
}
