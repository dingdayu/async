package async

// UnRegisterSignal represents a signal that can be sent to a Handle.
type UnRegisterSignal struct{}

// Signal triggers the exit behavior for UnRegisterSignal.
func (s UnRegisterSignal) Signal() {}

// String returns the string representation of UnRegisterSignal.
func (s UnRegisterSignal) String() string {
	return "unregister signal"
}

// ExitSignal represents a signal that indicates a Handle should exit.
type ExitSignal struct{}

// Signal triggers the exit behavior for ExitSignal.
func (s ExitSignal) Signal() {}

// String returns the string representation of ExitSignal.
func (s ExitSignal) String() string {
	return "exit signal"
}
