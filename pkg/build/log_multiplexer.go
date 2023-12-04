package build

import (
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

type LogID = string

type LogMessage struct {
	Id    LogID
	Bytes []byte
}

type ChannelWriter struct {
	id  string
	sub chan LogMessage
}

func (c *ChannelWriter) Write(bytes []byte) (int, error) {
	fmt.Println("writing new build output to channel")
	c.sub <- LogMessage{
		Id:    c.id,
		Bytes: bytes,
	}

	return len(bytes), nil
}

type Multiplexer struct {
	logsStream chan LogMessage
}

func (m *Multiplexer) Update() tea.Cmd {
	return func() tea.Msg {
		fmt.Println("waiting on log stream")
		msg := <-m.logsStream
		fmt.Printf("got log stream message %s\n%s\n", msg.Id, string(msg.Bytes))
		return msg
	}
}

func (m *Multiplexer) CreateWriter(ID string) io.Writer {
	return &ChannelWriter{
		id:  ID,
		sub: m.logsStream,
	}
}

func NewLogMultiplexer() Multiplexer {
	multiplexer := Multiplexer{
		logsStream: make(chan LogMessage),
	}

	return multiplexer
}
