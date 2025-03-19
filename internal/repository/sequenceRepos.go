package repository

import (
	"fmt"
	"strconv"

	"github.com/dgraph-io/badger/v3"
)

func (ir *IndexRepository) TransferOrSaveToSequence(words []string) ([]int, error) {
	var ids []int
    err := ir.db.Update(func(txn *badger.Txn) error {
        for _, word := range words {
            key := []byte("word:" + word)
            item, err := txn.Get(key)
            if err == nil {
                idBytes, err := item.ValueCopy(nil)
                if err != nil {
                    return err
                }
                id, err := strconv.Atoi(string(idBytes))
                if err != nil {
                    return err
                }
                ids = append(ids, id)
            } else if err == badger.ErrKeyNotFound {
                maxIdItem, err := txn.Get([]byte("max_id"))
                if err != nil && err != badger.ErrKeyNotFound {
                    return err
                }
                var maxId int
                if err == nil {
                    maxIdBytes, err := maxIdItem.ValueCopy(nil)
                    if err != nil {
                        return err
                    }
                    maxId, err = strconv.Atoi(string(maxIdBytes))
                    if err != nil {
                        return err
                    }
                }
                newId := maxId + 1
                idKey := fmt.Appendf(nil, "id:%d", newId)
                err = txn.Set(idKey, []byte(word))
                if err != nil {
                    return err
                }
                err = txn.Set(key, []byte(strconv.Itoa(newId)))
                if err != nil {
                    return err
                }
                err = txn.Set([]byte("max_id"), []byte(strconv.Itoa(newId)))
                if err != nil {
                    return err
                }
                ids = append(ids, newId)
            } else {
                return err
            }
        }
        return nil
    })
    if err != nil {
        return nil, err
    }
    return ids, nil
}

func (ir *IndexRepository) SequenceToWords(ids []int) ([]string, error) {
	var words []string
    err := ir.db.View(func(txn *badger.Txn) error {
        for _, id := range ids {
            key := fmt.Appendf(nil, "id:%d", id)
            item, err := txn.Get(key)
            if err == nil {
                wordBytes, err := item.ValueCopy(nil)
                if err != nil {
                    return err
                }
                words = append(words, string(wordBytes))
            } else if err == badger.ErrKeyNotFound {
                words = append(words, "")
            } else {
                return err
            }
        }
        return nil
    })
    if err != nil {
        return nil, err
    }
    return words, nil
}

func (ir *IndexRepository) GetLastId() (int, error) {
	var maxId int
    err := ir.db.View(func(txn *badger.Txn) error {
        item, err := txn.Get([]byte("max_id"))
        if err == badger.ErrKeyNotFound {
            maxId = 0
            return nil
        } else if err != nil {
            return err
        }
        maxIdBytes, err := item.ValueCopy(nil)
        if err != nil {
            return err
        }
        maxId, err = strconv.Atoi(string(maxIdBytes))
        if err != nil {
            return err
        }
        return nil
    })
    if err != nil {
        return 0, err
    }
    return maxId, nil
}