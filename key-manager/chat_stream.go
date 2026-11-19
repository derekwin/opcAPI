package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

type tokenUsage struct {
	Prompt     int64  `json:"prompt_tokens"`
	Completion int64  `json:"completion_tokens"`
	Total      *int64 `json:"total_tokens"`
}

type streamUsage struct {
	total   int64
	prompt  int64
	outputs map[int]int64
}

// SGLang sends one data line per event. Keep the original bytes for deltas,
// tool calls and [DONE] unless removing internally requested usage.
func (u *streamUsage) filter(line []byte, c *chatRequest) []byte {
	data, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok {
		return line
	}
	var chunk struct {
		Choices []struct{ Index int }
		Usage   *tokenUsage
	}
	if !bytes.Contains(data, []byte(`"usage"`)) || json.Unmarshal(data, &chunk) != nil || chunk.Usage == nil || chunk.Usage.Total == nil {
		return line
	}
	if len(chunk.Choices) == 0 {
		u.total = *chunk.Usage.Total
		if !c.includeUsage {
			return nil
		}
		return line
	}
	if len(chunk.Choices) == 1 {
		// Each partial update is cumulative for its own choice. Count the
		// shared prompt once if the stream stops before the final aggregate.
		i := chunk.Choices[0].Index
		u.prompt = max(u.prompt, chunk.Usage.Prompt)
		u.outputs[i] = max(u.outputs[i], chunk.Usage.Completion)
	}
	if c.continuous {
		return line
	}
	var fields rawObject
	if json.Unmarshal(data, &fields) != nil {
		return line
	}
	delete(fields, "usage")
	b, _ := json.Marshal(fields)
	return append(append([]byte("data: "), b...), '\n')
}

func relayStream(w http.ResponseWriter, body io.Reader, c *chatRequest) int64 {
	reader := bufio.NewReader(body)
	usage := streamUsage{outputs: make(map[int]int64)}
	controller := http.NewResponseController(w)
	hidden := false
readLoop:
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			filtered := usage.filter(line, c)
			switch {
			case filtered == nil:
				hidden = true
			case hidden && len(bytes.TrimSpace(line)) == 0:
				hidden = false
			default:
				hidden = false
				if _, writeErr := w.Write(filtered); writeErr != nil {
					break readLoop
				}
				if len(bytes.TrimSpace(line)) == 0 && controller.Flush() != nil {
					break readLoop
				}
			}
		}
		if err != nil {
			break
		}
	}
	if usage.total > 0 {
		return usage.total
	}
	total := usage.prompt
	for _, n := range usage.outputs {
		total += n
	}
	return total
}
