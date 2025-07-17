package service

import "sync"

// Service keeps public keys or ciphertext blobs if you want them later.
// For a pure pairing demo you can even ignore these methods –
// they’re here so you can reuse the file when you add push/pull logic.
type Service struct {
	mu       sync.RWMutex
	pubKeys  map[string]string   // appID → Base64 public key
	msgQueue map[string][]string // appID → ciphertext blobs
}

func NewService() *Service {
	return &Service{
		pubKeys:  make(map[string]string),
		msgQueue: make(map[string][]string),
	}
}

func (s *Service) RegisterKey(appID, pub string) { s.mu.Lock(); s.pubKeys[appID] = pub; s.mu.Unlock() }
func (s *Service) GetKey(appID string) (string, bool) {
	s.mu.RLock()
	v, ok := s.pubKeys[appID]
	s.mu.RUnlock()
	return v, ok
}
func (s *Service) PushBlob(appID, blob string) {
	s.mu.Lock()
	s.msgQueue[appID] = append(s.msgQueue[appID], blob)
	s.mu.Unlock()
}
func (s *Service) PullBlobs(appID string) []string {
	s.mu.Lock()
	q := s.msgQueue[appID]
	s.msgQueue[appID] = nil
	s.mu.Unlock()
	return q
}
