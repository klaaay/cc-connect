package codex

import "github.com/chenhg5/cc-connect/core"

// RPC 的读循环不能等待平台发送，也不能丢弃正文、权限请求或终态。
// 通道拥塞时按顺序暂存，由独立消费者转发；Close 负责取消和回收。
func (s *appServerSession) emit(event core.Event) {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	if s.eventClosed {
		return
	}
	if s.eventDone == nil {
		select {
		case s.events <- event:
			return
		default:
		}
		s.eventWake = make(chan struct{}, 1)
		s.eventStop = make(chan struct{})
		s.eventDone = make(chan struct{})
		go s.forwardEvents()
	}
	s.eventBacklog = append(s.eventBacklog, event)
	select {
	case s.eventWake <- struct{}{}:
	default:
	}
}

func (s *appServerSession) forwardEvents() {
	defer close(s.eventDone)
	for {
		s.eventMu.Lock()
		if s.eventClosed {
			s.eventMu.Unlock()
			return
		}
		if len(s.eventBacklog) == 0 {
			s.eventBacklog = nil
			s.eventMu.Unlock()
			select {
			case <-s.eventWake:
				continue
			case <-s.eventStop:
				return
			}
		}
		event := s.eventBacklog[0]
		s.eventBacklog[0] = core.Event{}
		s.eventBacklog = s.eventBacklog[1:]
		s.eventMu.Unlock()
		select {
		case s.events <- event:
		case <-s.eventStop:
			return
		}
	}
}

func (s *appServerSession) closeEvents() {
	s.eventMu.Lock()
	s.eventClosed = true
	done := s.eventDone
	if done != nil {
		close(s.eventStop)
	}
	s.eventMu.Unlock()
	if done != nil {
		<-done
	}
	s.eventMu.Lock()
	s.eventBacklog = nil
	close(s.events)
	s.eventMu.Unlock()
}
