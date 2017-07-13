package metering

import (
	"fmt"
	"strconv"

	log "github.com/Sirupsen/logrus"
	"github.com/boltdb/bolt"
)

var db *bolt.DB

func init() {
	// If already initialized, just return
	if db != nil {
		return
	}
	meterdb, err := bolt.Open("meter.db", 0600, nil)
	if err != nil {
		panic(err)
	}
	db = meterdb

	// TODO Start token removal process
}

// NewMeter returns a new Meter for the given token
// Database is structured as follows:
// Each token has a Bucket with the token id as name.
// Each Bucket has the following properties:
// * Credits
func NewMeter(id string, token string, expiration int, credits int) (*Meter, error) {
	err := db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(id))
		if err != nil {
			return err
		}

		// Token cannot be re-used, fill only if it is unseen
		tokens, err := b.CreateBucketIfNotExists([]byte("Tokens"))
		if err != nil {
			return err
		}

		// Check if token hasn't already been used
		if t := tokens.Get([]byte(token)); t == nil {
			// Add token to bucket
			tokens.Put([]byte(token), []byte(strconv.Itoa(expiration)))
			// Add credits to bucket
			c := b.Get([]byte("Credits"))
			if c != nil {
				balance, err := strconv.Atoi(string(c))
				if err != nil {
					return err
				}
				log.WithField("balance", balance).WithField("credits", credits).Info("Inserting new credits")
				b.Put([]byte("Credits"), []byte(strconv.Itoa(balance+credits)))
			} else {
				log.WithField("credits", credits).Info("Inserting new credits")
				b.Put([]byte("Credits"), []byte(strconv.Itoa(credits)))
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return &Meter{ID: id, db: db}, nil
}

// Meter will keep track of resources for a given minion or daemon
type Meter struct {
	ID string
	db *bolt.DB
}

// Status contains information about the current status of the resources of a token
type Status struct {
	Credits int
}

// Credits returns the remaining credits for the meter
func (m *Meter) Credits() (int, error) {
	ints, err := m.readIntegers("Credits")
	if err != nil {
		return 0, err
	}
	if len(ints) != 1 {
		return 0, fmt.Errorf("Unexpected amount of ints returned from db")
	}
	return ints[0], nil
}

func (m *Meter) readIntegers(keys ...string) ([]int, error) {
	values := make([]int, len(keys))
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(m.ID))
		if b != nil {
			for i := 0; i < len(keys); i++ {
				res := b.Get([]byte(keys[i]))
				if res != nil {
					value, err := strconv.Atoi(string(res))
					if err != nil {
						return err
					}
					values[i] = value
				}
			}
			return nil
		}
		return fmt.Errorf("Could not find bucket")
	})
	if err != nil {
		return nil, err
	}
	return values, nil
}

// Record will record the usage of the given amount of credits
func (m *Meter) Record(credits int) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(m.ID))
		if b != nil {
			var remaining int
			// Get remaining
			res := b.Get([]byte("Credits"))
			if res != nil {
				c, err := strconv.Atoi(string(res))
				if err != nil {
					return err
				}
				remaining = c
			}
			// Do not record if >= 0
			if remaining <= 0 {
				return fmt.Errorf("Could not record time - no credits left")
			}

			// Update
			err := b.Put([]byte("Credits"), []byte(strconv.Itoa(remaining-credits)))
			if err != nil {
				return err
			}
			log.WithField("remaining", remaining-credits).Info("Recorded some credit usage")
			return nil
		}
		return fmt.Errorf("Could not update for given id")
	})
}

// Inspect returns the status of the meter, incl:
// * Credits - the remaining credits
func (m *Meter) Inspect() (*Status, error) {
	credits, err := m.Credits()
	if err != nil {
		return nil, err
	}
	return &Status{Credits: credits}, nil
}
