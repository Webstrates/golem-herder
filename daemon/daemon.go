package daemon

// SpawnOptions contains configuration options for the daemon spawn.
type SpawnOptions struct {
	Ports  []int
	StdOut chan string
	StdErr chan string
}

// Spawn a daemon with the given options..
func Spawn(name, image string, options SpawnOptions) {

}
