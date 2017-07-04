package cmd

import (
	"fmt"

	"github.com/Webstrates/golem-herder/token"
	jwt "github.com/dgrijalva/jwt-go"
	"github.com/spf13/cobra"
)

var (
	credits int
	email   string
)

// serveCmd represents the serve command
var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Generate a token",
	Run: func(cmd *cobra.Command, args []string) {

		// Validation
		m, err := token.NewManager(pubKey, privKey)
		if err != nil {
			panic(err)
		}

		token, err := m.Generate(email, jwt.MapClaims{"crd": credits})
		if err != nil {
			panic(err)
		}

		fmt.Println(token)

	},
}

func init() {
	RootCmd.AddCommand(tokenCmd)

	tokenCmd.Flags().IntVarP(&credits, "crd", "c", 3e4, "How many credits do you want in your token?")
	tokenCmd.Flags().StringVarP(&email, "email", "e", "", "What is your email?")
}
