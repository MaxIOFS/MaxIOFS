package object

import "sync"

type multipartLock struct {
	gate  sync.RWMutex
	parts map[int]*sync.Mutex
	refs  int
}

// References include waiters, so an upload never acquires two independent locks.
func (om *objectManager) acquireUploadLock(id string, part int) (*multipartLock, *sync.Mutex, func()) {
	om.uploadsMu.Lock()
	if om.uploads == nil {
		om.uploads = make(map[string]*multipartLock)
	}
	l := om.uploads[id]
	if l == nil {
		l = &multipartLock{parts: make(map[int]*sync.Mutex)}
		om.uploads[id] = l
	}
	l.refs++
	var p *sync.Mutex
	if part > 0 {
		p = l.parts[part]
		if p == nil {
			p = &sync.Mutex{}
			l.parts[part] = p
		}
	}
	om.uploadsMu.Unlock()
	return l, p, func() {
		om.uploadsMu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(om.uploads, id)
		}
		om.uploadsMu.Unlock()
	}
}

func (om *objectManager) lockUpload(id string) func() {
	l, _, release := om.acquireUploadLock(id, 0)
	l.gate.Lock()
	return func() { l.gate.Unlock(); release() }
}

func (om *objectManager) lockUploadPart(id string, part int) func() {
	l, p, release := om.acquireUploadLock(id, part)
	l.gate.RLock()
	p.Lock()
	return func() { p.Unlock(); l.gate.RUnlock(); release() }
}
