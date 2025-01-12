/*
Package parse is a Go library for parsing Bro logs and working with
Bro log data.
*/
package parse

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Parser manages the structure of a Bro log.
// Fields and rows are represented by a slice, and the indexes in both the
// fields and row slices, share a 1 to 1 mapping.
// Ex: fields[0] is the value at row[0]
// FieldsIndex, is only used when a specific set of fields are
// selected to be parsed. These are defined in config/config.toml
// The allFields field determins whether you want to use specifc fields from the config
// or all of the fields in the Bro log.
// Augmented values are produced by defining specific Parse() functions.
type Parser struct {
	allFields   bool
	fields      []string
	fieldsIndex []int
	filepath    string
	Row         chan []string
}

// NewParser validates the Bro log exists and returns a new parser
// to perform parsing actions on.
func NewParser(path string, allFields bool) (*Parser, error) {

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, errors.New("File path does not exist")
	}

	p := new(Parser)
	p.filepath = path
	p.allFields = allFields
	return p, nil
}

// SetFields assigns the fields to be parsed.
func (p *Parser) SetFields(fields []string) {
	p.fields = fields
}

// Fields returns the fields of a bro log.
func (p *Parser) Fields() []string {
	return p.fields
}

// FieldsToUnderscore returns a new slice with "." replaced with "_".
func (p *Parser) FieldsToUnderscore() ([]string, error) {
	var underScoreFields []string

	if p.fields == nil {
		return nil, errors.New("No fields to replace")
	}

	for _, field := range p.fields {
		s := strings.Replace(field, ".", "_", -1)
		underScoreFields = append(underScoreFields, s)
	}

	return underScoreFields, nil
}

// GetIndexOfFields creates a slice that contains the index of specific
// fields to be parsed.
func (p *Parser) GetIndexOfFields() error {

	allFields, err := p.ParseAllFields()
	if err != nil {
		return err
	}

	if p.fields == nil {
		return errors.New("No specific fields defined for parsing")
	}

	// loop through specific fields
	for _, configField := range p.fields {
		index, err := getIndex(allFields, configField)
		if err != nil {
			return err
		}
		p.fieldsIndex = append(p.fieldsIndex, index)
	}

	return nil
}

// GetIndex returns the index of a specific element in a slice.
func getIndex(allFields []string, configField string) (int, error) {
	for i, field := range allFields {
		if field == configField {
			return i, nil
		}
	}

	return -1, errors.New("Couldn't match field defined in config with one in bro log, field is: " + configField)
}

// TODO remove hardcoding of the seperator, it could be something
// other than tabs (research this)?

// ParseAllFields parses the fields of a bro log, and stores them in a
// slice. Their positions in the bro log correspond to their index's
// in the slice.
func (p *Parser) ParseAllFields() ([]string, error) {
	var fields []string

	file, fileErr := os.Open(p.filepath)
	if fileErr != nil {
		return nil, fileErr
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		if line[0:7] == "#fields" {

			if line[8:] == "" {
				return nil, errors.New("Fields row is malformed")
			}

			fields = strings.Split(line[8:], "\t")
			break
		}

	}

	return fields, nil
}

// CountLines counts the number of lines in a file.
// Taken from
// http://stackoverflow.com/questions/24562942/golang-how-do-i-determine-the-number-of-lines-in-a-file-efficiently.
func (p *Parser) CountLines() (int, error) {

	file, fileErr := os.Open(p.filepath)
	if fileErr != nil {
		return -1, fileErr
	}
	defer file.Close()

	buf := make([]byte, 32*1024)
	count := 0
	lineSep := []byte{'\n'}

	for {
		c, err := file.Read(buf)
		count += bytes.Count(buf[:c], lineSep)

		switch {
		case err == io.EOF:
			return count, nil

		case err != nil:
			return count, err
		}
	}
}

// AutoCreateBuffer is a wrapper to initialize the buffer with a size equivalent
// to the number of lines in a log file.
func (p *Parser) AutoCreateBuffer() error {

	lineNum, err := p.CountLines()
	if err != nil {
		return err
	}
	p.CreateBuffer(lineNum)
	return nil
}

// CreateBuffer initializes the buffer. Without initialization, the channel
// will block on reads.
func (p *Parser) CreateBuffer(bufferSize int) {
	p.Row = make(chan []string, bufferSize)
}

// Parse is used as an optional argument to BufferRow, and can be used
// to perform additonal logic on the Bro log data.
type Parse func([]string, []string) ([]string, error)

// BufferRow parses throught the entries (data) of a Bro log,
// pushes them into the channel p.Row. There are two options
// to configure what will be pushed into p.Row.
// Whether specific fields are defined to be parsed.
// And whether certain fields require extra data manipulation.
// For extra data manipulation a Parse() function must be defined and
// passed into BufferRow.
func (p *Parser) BufferRow(parseFunc ...Parse) {

	if p.Row == nil {
		fmt.Println("Initialize nil channel, via CreateBuffer()")
		return
	}

	if p.fields == nil {
		fmt.Println("No fields parsed")
		return
	}

	if p.allFields == false {
		err := p.GetIndexOfFields()
		if err != nil {
			fmt.Println(err)
			return
		}
	}

	var moreDataFiltering bool
	if len(parseFunc) == 0 {
		moreDataFiltering = false
	} else {
		moreDataFiltering = true
	}

	file, fileErr := os.Open(p.filepath)
	if fileErr != nil {
		fmt.Println(fileErr)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		// Any line without a # is a row with values
		if string(line[0]) != "#" {

			// Lets make sure the value row is not malformed
			if line[1:] == "" {
				continue
			}

			entry := strings.Split(line, "\t")

			// Do we have specific fields we want to parse
			if p.allFields == false {
				var parsedEntry []string
				for _, fieldIndex := range p.fieldsIndex {
					parsedEntry = append(parsedEntry, entry[fieldIndex])
				}

				// Do we just want the raw entries
				if moreDataFiltering == false {
					p.Row <- parsedEntry
				} else {
					modifiedParsedEntry, err := parseFunc[0](p.fields, parsedEntry)
					if err != nil {
						p.Row <- parsedEntry
					} else {
						p.Row <- modifiedParsedEntry
					}

				}
			} else {
				// Skip this line if columns and values don't match
				if len(p.fields) != len(entry) {
					continue
				}
				// Do we just want the raw entries
				if moreDataFiltering == false {
					p.Row <- entry
				} else {
					modifiedParsedEntry, err := parseFunc[0](p.fields, entry)
					if err != nil {
						p.Row <- entry
					} else {
						p.Row <- modifiedParsedEntry
					}
				}

			}

		}

	}

	close(p.Row)
}
