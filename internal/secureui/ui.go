package secureui

type Native struct{}

func (Native) Notice(t, s string)                            { Notice(t, s) }
func (Native) Input(t string, l ...string) ([][]byte, error) { return Input(t, l...) }
func (Native) Confirm(t, s string) bool                      { return Confirm(t, s) }
func (Native) File(t string, s bool) (string, error)         { return File(t, s) }
func (Native) Receive(t, s string, p []byte) bool            { return Receive(t, s, p) }
func (Native) ReceiveAddress(t, s, a string, p []byte) bool  { return ReceiveAddress(t, s, a, p) }
func (Native) Recovery(t string, p []byte) bool              { return Recovery(t, p) }
