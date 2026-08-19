package sstp

import (
	"fmt"
	"net"
	"strings"
	"time"
)

func SendScript(sender, script string) error {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:9801", 1200*time.Millisecond)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	msg := "SEND SSTP/1.4\r\nSender: " + sender + "\r\nCharset: UTF-8\r\nScript: " + strings.ReplaceAll(script, "\r", "") + "\r\n\r\n"
	_, err = fmt.Fprint(conn, msg)
	return err
}

func RaiseEvent(sender, event string, refs ...string) error {
	var b strings.Builder
	b.WriteString("\\![raise,")
	b.WriteString(event)
	for _, r := range refs {
		b.WriteByte(',')
		b.WriteString(EscapeArg(r))
	}
	b.WriteString("]\\e")
	return SendScript(sender, b.String())
}

func EscapeArg(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "]", "\\]")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

func EscapeSakuraText(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "\r", "", "\n", "\\n")
	return r.Replace(s)
}
