package api

import (
	"encoding/json"
	"sync"
)

// Task status values shared by analyze and export SSE streams.
const (
	TaskRunning   = "running"
	TaskCompleted = "completed"
	TaskFailed    = "failed"
)

// TaskEvent is the payload streamed over SSE and kept as the latest state.
type TaskEvent struct {
	Kind      string          `json:"kind"`
	TaskID    string          `json:"task_id"`
	ProjectID string          `json:"project_id"`
	Status    string          `json:"status"`
	Progress  float64         `json:"progress"`
	Message   string          `json:"message,omitempty"`
	Error     string          `json:"error,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

type taskState struct {
	event  TaskEvent
	subs   map[chan TaskEvent]struct{}
	closed bool
}

type taskHub struct {
	mu    sync.Mutex
	tasks map[string]*taskState
}

func newTaskHub() *taskHub {
	return &taskHub{tasks: map[string]*taskState{}}
}

func taskKey(kind, projectID string) string { return kind + ":" + projectID }

type taskHandle struct {
	hub *taskHub
	key string
	id  string
}

func (h *taskHub) begin(kind, projectID, taskID string) *taskHandle {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tasks[taskKey(kind, projectID)] = &taskState{
		event: TaskEvent{
			Kind:      kind,
			TaskID:    taskID,
			ProjectID: projectID,
			Status:    TaskRunning,
			Progress:  0,
			Message:   "Memulai...",
		},
		subs: map[chan TaskEvent]struct{}{},
	}
	return &taskHandle{hub: h, key: taskKey(kind, projectID), id: taskID}
}

func (h *taskHub) current(kind, projectID string) (TaskEvent, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	state, ok := h.tasks[taskKey(kind, projectID)]
	if !ok {
		return TaskEvent{}, false
	}
	return state.event, true
}

func (h *taskHub) running(kind, projectID string) bool {
	ev, ok := h.current(kind, projectID)
	return ok && ev.Status == TaskRunning
}

func (h *taskHub) subscribe(kind, projectID string) (<-chan TaskEvent, func(), bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	state, ok := h.tasks[taskKey(kind, projectID)]
	if !ok {
		return nil, nil, false
	}
	ch := make(chan TaskEvent, 16)
	ch <- state.event
	if !state.closed {
		state.subs[ch] = struct{}{}
	}
	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if state, ok := h.tasks[taskKey(kind, projectID)]; ok {
			delete(state.subs, ch)
		}
	}
	return ch, cancel, true
}

func (t *taskHandle) publish(mutate func(*TaskEvent)) {
	t.hub.mu.Lock()
	defer t.hub.mu.Unlock()
	state, ok := t.hub.tasks[t.key]
	if !ok || state.event.TaskID != t.id {
		return
	}
	mutate(&state.event)
	for ch := range state.subs {
		select {
		case ch <- state.event:
		default:
		}
	}
	if state.event.Status != TaskRunning {
		for ch := range state.subs {
			close(ch)
			delete(state.subs, ch)
		}
		state.closed = true
	}
}

func (t *taskHandle) Update(progress float64, message string) {
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	t.publish(func(ev *TaskEvent) {
		ev.Progress = progress
		if message != "" {
			ev.Message = message
		}
	})
}

func (t *taskHandle) Complete(result any) {
	var raw json.RawMessage
	if result != nil {
		if data, err := json.Marshal(result); err == nil {
			raw = data
		}
	}
	t.publish(func(ev *TaskEvent) {
		ev.Status = TaskCompleted
		ev.Progress = 1
		ev.Message = "Selesai."
		ev.Result = raw
	})
}

func (t *taskHandle) Fail(err error) {
	msg := "Terjadi kesalahan."
	if err != nil {
		msg = err.Error()
	}
	t.publish(func(ev *TaskEvent) {
		ev.Status = TaskFailed
		ev.Error = msg
		ev.Message = "Gagal."
	})
}
