package utils

import (
	logging "Metarr/internal/utils/logging"
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

var (
	userInputChan = make(chan string) // Channel for user input
	decisionMade  bool
)

// Initialize user input reader in a goroutine
func InitUserInputReader() {
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			input, _ := reader.ReadString('\n')
			userInputChan <- strings.TrimSpace(input)
		}
	}()
}

// PromptMetaReplace displays a prompt message and waits for valid user input.
// The option can be used to tell the program to overwrite all in the queue,
// preserve all in the queue, or move through value by value
func PromptMetaReplace(promptMsg, fileName string, overwriteAll, preserveAll *bool) (string, error) {

	logging.PrintD(3, "Entering PromptUser dialogue...")
	ctx := context.Background()

	if decisionMade {
		// If overwriteAll, return "Y" without waiting
		if *overwriteAll {

			logging.PrintD(3, "Overwrite all is set...")
			return "Y", nil
		} else if *preserveAll {

			logging.PrintD(3, "Preserve all is set...")
			return "N", nil
		}
	}

	fmt.Println()
	logging.PrintI(promptMsg)

	// Wait for user input
	select {
	case response := <-userInputChan:
		if response == "Y" {
			*overwriteAll = true
		}
		decisionMade = true
		return response, nil

	case <-ctx.Done():
		logging.PrintI("Operation canceled during input.")
		return "", fmt.Errorf("operation canceled")
	}
}
