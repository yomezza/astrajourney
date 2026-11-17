package integration

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type mixedMetric struct {
	Count, Bytes int64
	Millis       []float64
	Codes        map[string]int64
}

type mixedMessage struct {
	Sent      time.Time
	Kind      string
	Ack, Seen bool
}

type mixedMetrics struct {
	stopActions atomic.Bool
	browserPID  atomic.Int64
	mu          sync.Mutex
	phase       string
	items       map[string]*mixedMetric
	counts      map[string]int64
	messages    map[string]*mixedMessage
}

func newMixedMetrics() *mixedMetrics {
	return &mixedMetrics{phase: "setup", items: map[string]*mixedMetric{}, counts: map[string]int64{}, messages: map[string]*mixedMessage{}}
}
func (m *mixedMetrics) setPhase(phase string) { m.mu.Lock(); m.phase = phase; m.mu.Unlock() }
func (m *mixedMetrics) count(key string, n int64) {
	m.mu.Lock()
	m.counts[m.phase+"/"+key] += n
	m.mu.Unlock()
}
func (m *mixedMetrics) record(name string, began time.Time, code string, bytes int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.phase + "/" + name
	v := m.items[key]
	if v == nil {
		v = &mixedMetric{Codes: map[string]int64{}}
		m.items[key] = v
	}
	v.Count++
	v.Bytes += int64(bytes)
	v.Codes[code]++
	v.Millis = append(v.Millis, float64(time.Since(began))/float64(time.Millisecond))
}
func (m *mixedMetrics) sentMessage(id, kind string) {
	m.mu.Lock()
	m.messages[id] = &mixedMessage{Sent: time.Now(), Kind: kind}
	m.mu.Unlock()
}
func (m *mixedMetrics) ackMessage(id string, ok bool) {
	m.mu.Lock()
	if v := m.messages[id]; v != nil {
		v.Ack = ok
	}
	m.mu.Unlock()
}
func (m *mixedMetrics) seeMessage(id string) {
	m.mu.Lock()
	v := m.messages[id]
	fresh := v != nil && !v.Seen
	var began time.Time
	if fresh {
		v.Seen = true
		began = v.Sent
	}
	m.mu.Unlock()
	if fresh {
		m.record("chat.send_to_first_history", began, "ok", 0)
	}
}
func mixedDistribution(values []float64) map[string]any {
	copyValues := append([]float64(nil), values...)
	sort.Float64s(copyValues)
	q := func(p float64) float64 {
		if len(copyValues) == 0 {
			return 0
		}
		return copyValues[max(0, int(math.Ceil(float64(len(copyValues))*p))-1)]
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := 0.0
	if len(values) > 0 {
		mean = sum / float64(len(values))
	}
	return map[string]any{"samples": len(values), "mean_ms": mean, "p50_ms": q(.5), "p95_ms": q(.95), "p99_ms": q(.99), "max_ms": q(1)}
}
func (m *mixedMetrics) snapshot() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := map[string]any{}
	for key, v := range m.items {
		d := mixedDistribution(v.Millis)
		d["count"] = v.Count
		d["body_bytes"] = v.Bytes
		codes := map[string]int64{}
		for key, count := range v.Codes {
			codes[key] = count
		}
		d["codes"] = codes
		items[key] = d
	}
	counts := map[string]int64{}
	for k, v := range m.counts {
		counts[k] = v
	}
	acked, seen, unseen := 0, 0, 0
	for _, v := range m.messages {
		if v.Ack {
			acked++
			if v.Seen {
				seen++
			} else if time.Since(v.Sent) > 10*time.Second {
				unseen++
			}
		}
	}
	return map[string]any{"phase": m.phase, "metrics": items, "counts": counts, "chat_sent": len(m.messages), "chat_ack_ok": acked, "chat_ack_seen_in_history": seen, "chat_ack_unseen_over_10s": unseen}
}
func (m *mixedMetrics) failures() (int64, int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var all, failed int64
	for key, v := range m.items {
		if !strings.HasPrefix(key, m.phase+"/http ") {
			continue
		}
		all += v.Count
		for code, n := range v.Codes {
			if code == "transport" || strings.HasPrefix(code, "5") {
				failed += n
			}
		}
	}
	return all, failed
}
func (m *mixedMetrics) activity() map[string]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]int64{"chat_sent": int64(len(m.messages))}
	for key, v := range m.items {
		if strings.Contains(key, "/http ") {
			out["http_requests"] += v.Count
			out["http_body_bytes"] += v.Bytes
			for code, count := range v.Codes {
				if code == "transport" || strings.HasPrefix(code, "5") {
					out["http_5xx_transport"] += count
				}
			}
		}
	}
	for _, v := range m.messages {
		if v.Ack {
			out["chat_ack_ok"]++
		}
	}
	return out
}
func mixedWriteJSON(name string, value any) error {
	raw, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	tmp := name + ".partial"
	if e = os.WriteFile(tmp, append(raw, '\n'), 0600); e != nil {
		return e
	}
	return os.Rename(tmp, name)
}
func mixedCgroup() map[string]string {
	out := map[string]string{}
	for _, name := range []string{"cpu.max", "cpu.stat", "memory.max", "memory.current", "memory.peak", "cpu.pressure", "memory.pressure", "io.pressure"} {
		if b, e := os.ReadFile("/sys/fs/cgroup/" + name); e == nil {
			out[name] = strings.TrimSpace(string(b))
		}
	}
	return out
}
func mixedChildren(pid int) []int {
	b, e := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/children", pid, pid))
	if e != nil {
		// Some managed Linux environments omit task/*/children in /proc.
		entries, _ := os.ReadDir("/proc")
		out := []int{}
		for _, entry := range entries {
			child, e := strconv.Atoi(entry.Name())
			if e != nil {
				continue
			}
			raw, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", child))
			if e != nil {
				continue
			}
			_, tail, _ := strings.Cut(string(raw), ") ")
			fields := strings.Fields(tail)
			if len(fields) >= 2 && fields[1] == strconv.Itoa(pid) {
				out = append(out, child)
			}
		}
		return out
	}
	out := []int{}
	for _, word := range strings.Fields(string(b)) {
		var n int
		if _, e = fmt.Sscan(word, &n); e == nil {
			out = append(out, n)
		}
	}
	return out
}
func mixedProcessGroup(pid int) processUsage230 {
	children := map[int][]int{}
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		child, e := strconv.Atoi(entry.Name())
		if e != nil {
			continue
		}
		raw, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", child))
		if e != nil {
			continue
		}
		_, tail, _ := strings.Cut(string(raw), ") ")
		fields := strings.Fields(tail)
		if len(fields) >= 2 {
			parent, _ := strconv.Atoi(fields[1])
			children[parent] = append(children[parent], child)
		}
	}
	var v processUsage230
	var visit func(int)
	visit = func(current int) {
		next := processUsageLoad230(current)
		v.CPUSeconds += next.CPUSeconds
		v.RSSBytes += next.RSSBytes
		for _, child := range children[current] {
			visit(child)
		}
	}
	visit(pid)
	return v
}
func mixedLogSizes(directory string) map[string]int64 {
	out := map[string]int64{}
	entries, _ := os.ReadDir(directory)
	for _, entry := range entries {
		if info, e := entry.Info(); e == nil && !info.IsDir() {
			out[filepath.Base(entry.Name())] = info.Size()
		}
	}
	return out
}
