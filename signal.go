package async

type UnRegisterSignal struct{}

func (s UnRegisterSignal) Signal() {}

// String
func (s UnRegisterSignal) String() string {
	return "unregister signal"
}

type ExitSignal struct{}

func (s ExitSignal) Signal() {}

// String
func (s ExitSignal) String() string {
	return "exit signal"
}
