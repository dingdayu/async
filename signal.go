package async

// UnRegisterSignal
type UnRegisterSignal struct {
}

// Signal
func (s UnRegisterSignal) Signal() {}

// String
func (s UnRegisterSignal) String() string {
	return "unregister signal"
}
