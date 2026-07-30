package ws

import (
	"encoding/json"
	"fmt"
	"sync"
)

type StepRecord struct {
	Version  int64           `json:"version"`
	Step     json.RawMessage `json:"step"`
	ClientID string          `json:"client_id"`
}

type OTDocument struct {
	Version int64
	Content string
	Steps   []StepRecord
}

type OTEngine struct {
	mu   sync.RWMutex
	docs map[string]*OTDocument
}

func NewOTEngine() *OTEngine {
	return &OTEngine{docs: make(map[string]*OTDocument)}
}

func (e *OTEngine) InitDocument(title string, content string) *OTDocument {
	e.mu.Lock()
	defer e.mu.Unlock()

	if doc := e.docs[title]; doc != nil {
		return cloneDocument(doc)
	}

	doc := &OTDocument{Content: content}
	e.docs[title] = doc
	return cloneDocument(doc)
}

func (e *OTEngine) ResetDocument(title string, content string) *OTDocument {
	e.mu.Lock()
	defer e.mu.Unlock()

	old := e.docs[title]
	version := int64(0)
	if old != nil {
		version = old.Version + 1
	}

	doc := &OTDocument{Version: version, Content: content}
	e.docs[title] = doc
	return cloneDocument(doc)
}

func (e *OTEngine) GetDocument(title string) *OTDocument {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return cloneDocument(e.docs[title])
}

func (e *OTEngine) ApplySteps(title string, clientID string, version int64, steps []json.RawMessage, content string) (accepted []StepRecord, missing []StepRecord, newVersion int64, ok bool, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	doc := e.docs[title]
	if doc == nil {
		doc = &OTDocument{}
		e.docs[title] = doc
	}

	if version < 0 || version > doc.Version {
		return nil, nil, doc.Version, false, fmt.Errorf("invalid OT version")
	}

	if version < doc.Version {
		missing := cloneStepsSince(doc, version)
		if int64(len(missing)) != doc.Version-version {
			return nil, nil, doc.Version, false, fmt.Errorf("OT history unavailable")
		}
		return nil, missing, doc.Version, false, nil
	}

	accepted = make([]StepRecord, 0, len(steps))
	for _, step := range steps {
		doc.Version++
		record := StepRecord{
			Version:  doc.Version,
			Step:     cloneRaw(step),
			ClientID: clientID,
		}
		doc.Steps = append(doc.Steps, record)
		accepted = append(accepted, record)
	}

	doc.Content = content

	return accepted, nil, doc.Version, true, nil
}

func cloneDocument(doc *OTDocument) *OTDocument {
	if doc == nil {
		return nil
	}
	steps := make([]StepRecord, len(doc.Steps))
	for i, step := range doc.Steps {
		steps[i] = StepRecord{
			Version:  step.Version,
			Step:     cloneRaw(step.Step),
			ClientID: step.ClientID,
		}
	}
	return &OTDocument{
		Version: doc.Version,
		Content: doc.Content,
		Steps:   steps,
	}
}

func cloneStepsSince(doc *OTDocument, version int64) []StepRecord {
	if doc == nil || version >= doc.Version {
		return nil
	}
	steps := make([]StepRecord, 0, int(doc.Version-version))
	for _, step := range doc.Steps {
		if step.Version > version {
			steps = append(steps, StepRecord{
				Version:  step.Version,
				Step:     cloneRaw(step.Step),
				ClientID: step.ClientID,
			})
		}
	}
	return steps
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	cp := make([]byte, len(raw))
	copy(cp, raw)
	return cp
}
