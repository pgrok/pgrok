package creator

type Option struct {
	protocol string
	host     string
	port     int
	user     string
	password string
	database string
}

func NewOption(protocol, host string, port int, user string, password string, database string) *Option {
	return &Option{
		protocol: protocol,
		host:     host,
		port:     port,
		user:     user,
		password: password,
		database: database,
	}
}
