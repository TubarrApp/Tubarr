package print

import (
	consts "Metarr/internal/domain/constants"
	"Metarr/internal/types"
	logging "Metarr/internal/utils/logging"
	"fmt"
	"reflect"
	"sync"
)

var muPrint sync.Mutex

// CreateModelPrintout prints out the values stored in a struct.
// taskName allows you to enter your own identifier for this task.
func CreateModelPrintout(model any, filename, taskName string, args ...interface{}) {
	muPrint.Lock()
	defer muPrint.Unlock()

	output := "\n\n================= " + consts.ColorCyan + "Printing metadata fields for:" + consts.ColorReset + " '" + consts.ColorReset + filename + "' =================\n"

	if taskName != "" {
		str := fmt.Sprintf("'"+taskName+"'", args...)
		output += "\n" + consts.ColorGreen + "Printing model at point of task " + consts.ColorReset + str + "\n"
	}

	// Add fields from the struct
	output += consts.ColorYellow + "\nFile Information:\n" + consts.ColorReset
	output += printStructFields(model)

	if m, ok := model.(*types.FileData); ok {
		output += consts.ColorYellow + "\nCredits:\n" + consts.ColorReset
		output += printStructFields(m.MCredits)

		output += consts.ColorYellow + "\nTitles and descriptions:\n" + consts.ColorReset
		output += printStructFields(m.MTitleDesc)

		output += consts.ColorYellow + "\nDates and timestamps:\n" + consts.ColorReset
		output += printStructFields(m.MDates)

		output += consts.ColorYellow + "\nWebpage data:\n" + consts.ColorReset
		output += printStructFields(m.MWebData)

		output += consts.ColorYellow + "\nShow data:\n" + consts.ColorReset
		output += printStructFields(m.MShowData)

		output += consts.ColorYellow + "\nOther data:\n" + consts.ColorReset
		output += printStructFields(m.MOther)
	} else if n, ok := model.(*types.NFOData); ok {
		output += consts.ColorYellow + "\nCredits:\n" + consts.ColorReset
		for _, actor := range n.Actors {
			output += printStructFields(actor.Name)
		}
		for _, director := range n.Directors {
			output += printStructFields(director)
		}
		for _, producer := range n.Producers {
			output += printStructFields(producer)
		}
		for _, publisher := range n.Publishers {
			output += printStructFields(publisher)
		}
		for _, studio := range n.Studios {
			output += printStructFields(studio)
		}
		for _, writer := range n.Writers {
			output += printStructFields(writer)
		}

		output += consts.ColorYellow + "\nTitles and descriptions:\n" + consts.ColorReset
		output += printStructFields(n.Title)
		output += printStructFields(n.Description)
		output += printStructFields(n.Plot)

		output += consts.ColorYellow + "\nWebpage data:\n" + consts.ColorReset
		output += printStructFields(n.WebpageInfo)

		output += consts.ColorYellow + "\nShow data:\n" + consts.ColorReset
		output += printStructFields(n.ShowInfo.Show)
		output += printStructFields(n.ShowInfo.EpisodeID)
		output += printStructFields(n.ShowInfo.EpisodeTitle)
		output += printStructFields(n.ShowInfo.SeasonNumber)
	}

	output += "\n\n================= " + consts.ColorYellow + "End metadata fields for:" + consts.ColorReset + " '" + filename + "' =================\n\n"

	logging.Print(output)
}

// Function to print the fields of a struct using reflection
func printStructFields(s interface{}) string {
	val := reflect.ValueOf(s)

	// Dereference pointer
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return fmt.Sprintf("Expected a struct, got %s\n", val.Kind())
	}

	typ := val.Type()
	output := ""

	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)      // Get field metadata
		fieldValue := val.Field(i) // Get field value

		// Skip zero or empty fields
		if fieldValue.IsZero() {
			output += field.Name + consts.ColorRed + " [empty]\n" + consts.ColorReset
			continue
		}

		fieldName := field.Name
		fieldValueStr := fmt.Sprintf("%v", fieldValue.Interface()) // Convert the value to a string

		// Append the field name and value in key-value format
		output += fmt.Sprintf("%s: %s\n", fieldName, fieldValueStr)
	}

	return output
}

// Print out the fetched fields
func PrintGrabbedFields(fieldType string, p *map[string]string) {

	printMap := *p

	muPrint.Lock()
	defer muPrint.Unlock()

	fmt.Println()
	logging.PrintI("Found and stored %s metadata fields from metafile:", fieldType)
	fmt.Println()

	for printKey, printVal := range printMap {
		if printKey != "" && printVal != "" {
			fmt.Printf(consts.ColorGreen + "Key: " + consts.ColorReset + printKey + consts.ColorYellow + "\nValue: " + consts.ColorReset + printVal + "\n")
		}
	}
	fmt.Println()
}
