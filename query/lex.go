package query

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// itemType identifies the type of lex items.
type itemType int

const eof = -1

const (
	itemNull  itemType = iota
	itemError          // error occurred;
	// value is text of error
	itemEOF
	itemLeftParen   // (
	itemRightParen  // )
	itemOr          // |
	itemAnd         // ,
	itemNot         // !
	itemLt          // <<
	itemLtEq        // <=, <
	itemGt          // >>
	itemGtEq        // >=, >
	itemEq          // =
	itemPatMatch    // %
	itemRegexp      // ~
	itemLeftCurly   // {
	itemRightCurly  // }
	itemLeftSquare  // [
	itemRightSquare // ]
	itemColon       // :
	itemString
	itemNumber
)

// item represents a token returned from the scanner.
type item struct {
	typ itemType // Type, such as itemNumber.
	val string   // Value, such as "23.2".
	num int      // Integer value
}

func (i item) String() string {
	if i.typ == itemString {
		return fmt.Sprintf("%#v", i.val)
	}
	if i.typ == itemEOF {
		return "<EOL>"
	}
	if i.typ == itemError {
		return fmt.Sprintf("error: %s", i.val)
	}
	if i.typ == itemNull {
		return "<NULL>"
	}
	return i.val
}

// stateFn represents the state of the scanner
// as a function that returns the next state.
type stateFn func(*lexer) stateFn

// lexer holds the state of the scanner.
type lexer struct {
	name        string    // used only for error reports.
	input       string    // the string being scanned.
	start       int       // start position of this item.
	pos         int       // current position in the input.
	width       int       // width of last rune read from input.
	squareLevel int       // level of square braces
	items       chan item // channel of scanned items.
	last        item
}

func lex(name, input string) (*lexer, chan item) {
	l := &lexer{
		name:  name,
		input: input,
		items: make(chan item),
	}
	go l.run() // Concurrently run state machine.
	return l, l.items
}

// emit passes an item back to the client.
func (l *lexer) emit(t itemType) {
	l.items <- item{t, l.input[l.start:l.pos], 0}
	l.start = l.pos
}

// run lexes the input by executing state functions until
// the state is nil.
func (l *lexer) run() {
	for state := lexMain; state != nil; {
		state = state(l)
	}
	close(l.items) // No more tokens will be delivered.
}

// next returns the next rune in the input.
func (l *lexer) next() (r rune) {
	if l.pos >= len(l.input) {
		l.width = 0
		return eof
	}
	r, l.width =
		utf8.DecodeRuneInString(l.input[l.pos:])
	l.pos += l.width
	return r
}

// ignore skips over the pending input before this point.
func (l *lexer) ignore() {
	l.start = l.pos
}

// backup steps back one rune.
// Can be called only once per call of next.
func (l *lexer) backup() {
	l.pos -= l.width
}

// peek returns but does not consume
// the next rune in the input.
func (l *lexer) peek() rune {
	r := l.next()
	l.backup()
	return r
}

func (l *lexer) Current() item {
	if l.last.typ == 0 {
		l.last = <-l.items
	}

	return l.last
}

func (l *lexer) Consume() {
	l.last = <-l.items
}

// error returns an error token and terminates the scan
// by passing back a nil pointer that will be the next
// state, terminating l.run.
func (l *lexer) errorf(format string, args ...interface{}) stateFn {
	l.items <- item{
		itemError,
		fmt.Sprintf(format, args...),
		0,
	}
	return nil
}

func lexMain(l *lexer) stateFn {
	switch r := l.next(); {
	case r == eof:
		l.emit(itemEOF)
		return nil
	case unicode.IsSpace(r):
		l.ignore()
	case r == '(':
		l.emit(itemLeftParen)
	case r == ')':
		l.emit(itemRightParen)
	case r == '{':
		l.emit(itemLeftCurly)
	case r == '}':
		l.emit(itemRightCurly)
	case r == '[':
		l.squareLevel++
		l.emit(itemLeftSquare)
	case r == ']':
		l.squareLevel--
		l.emit(itemRightSquare)
	case r == ':':
		l.emit(itemColon)
	case r == '|':
		l.emit(itemOr)
	case r == ',':
		l.emit(itemAnd)
	case r == '!':
		l.emit(itemNot)
	case r == '<':
		r2 := l.next()
		if r2 == '<' {
			l.emit(itemLt)
		} else if r2 == '=' {
			l.emit(itemLtEq)
		} else {
			l.backup()
			l.emit(itemLtEq)
		}
	case r == '>':
		r2 := l.next()
		if r2 == '>' {
			l.emit(itemGt)
		} else if r2 == '=' {
			l.emit(itemGtEq)
		} else {
			l.backup()
			l.emit(itemGtEq)
		}
	case r == '=':
		l.emit(itemEq)
	case r == '%':
		l.emit(itemPatMatch)
	case r == '~':
		l.emit(itemRegexp)
	case r == '+' || r == '-' || r == '0' || r == '1' || r == '2' || r == '3' ||
		r == '4' || r == '5' || r == '6' || r == '7' || r == '8' || r == '9':

		l.backup()
		if l.squareLevel > 0 {
			return lexNumber
		}

		return lexString
	default:
		l.backup()
		return lexString
	}

	return lexMain
}

func lexString(l *lexer) stateFn {
	r := l.next()
	// quoted string
	if r == '"' || r == '\'' {
		quote := r
		result := ""
		l.ignore()
		for {
			r = l.next()
			if r == quote {
				l.ignore()
				l.items <- item{itemString, result, 0}
				return lexMain
			}
			if r == '\\' {
				r = l.next()
			}
			if r == eof {
				return l.errorf("unexpected eof in quoted string")
			}
			result = result + string(r)
		}
	} else {
		// unquoted string
		for {
			if unicode.IsSpace(r) || strings.IndexRune("()|,!{}[]", r) > 0 {
				l.backup()
				l.emit(itemString)
				return lexMain
			}

			if r == eof {
				l.emit(itemString)
				l.emit(itemEOF)
				return nil
			}
			r = l.next()
		}
	}
}

func lexNumber(l *lexer) stateFn {
	r := l.next()
	numStr := ""

	if r == '+' || r == '-' {
		numStr += string(r)
		l.ignore()
		r = l.next()
	}

	for r >= '0' && r <= '9' {
		numStr += string(r)
		l.ignore()
		r = l.next()
	}

	num, err := strconv.Atoi(numStr)
	if err != nil {
		return l.errorf("invalid number: %s", err)
	}

	l.items <- item{itemNumber, "", num}

	l.backup()

	return lexMain
}
