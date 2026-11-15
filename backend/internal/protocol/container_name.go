package protocol

func ManagedContainerName(function, program, id string) string {
	return "opsd-" + function + "-" + program + "-" + id
}

func (d DatabaseDeployment) ContainerName() string {
	program := d.Kind
	if program == "galera" {
		program = "mariadb"
	}
	return ManagedContainerName("database", program, d.ID)
}

func (d DatabaseDeployment) LegacyContainerName() string {
	return "opsd-" + d.Kind + "-" + d.ID
}
