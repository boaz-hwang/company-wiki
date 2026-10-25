package wiki

func Setup(args []string, x IO) int                { return 1 }
func SessionCmd(args []string, x IO) int           { return 1 }
func ReviewCmd(c *Config, args []string, x IO) int { return 1 }
func InventoryCmd(c *Config, args []string, out interface{ Write([]byte) (int, error) }) int {
	return 1
}
