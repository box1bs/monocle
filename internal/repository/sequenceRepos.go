package repository

import (
	"strconv"

	"github.com/dgraph-io/badger/v3"
)

func (ir *IndexRepository) TransferToSequence(words ...string) ([]int, error) {
	var ids []int
    err := ir.DB.View(func(txn *badger.Txn) error {
        for _, word := range words {
            item, err := txn.Get([]byte("word:" + word))
            if err == badger.ErrKeyNotFound {
                ids = append(ids, -1)
                continue
            } else if err != nil {
                return err
            }
            idBytes, err := item.ValueCopy(nil)
            if err != nil {
                return err
            }
            id, err := strconv.Atoi(string(idBytes))
            if err != nil {
                return err
            }
            ids = append(ids, id)
        }
        return nil
    })
    if err != nil {
        return nil, err
    }
    return ids, nil
}

func (ir *IndexRepository) SaveToSequence(words ...string) ([]int, error) {
    sequence := make([]int, 0)

    current := 0
    txn := ir.DB.NewTransaction(true)

    for _, word := range words {
        if len(word) == 0 {
            continue
        }
        key := []byte(word)
        item, err := txn.Get(key)
        if err == nil {
            val, err := item.ValueCopy(nil)
            if err != nil {
                return nil, err
            }
            id, err := strconv.Atoi(string(val))
            if err != nil {
                return nil, err
            }
            sequence = append(sequence, id)
        } else if err == badger.ErrKeyNotFound {
            maxId, err := ir.getLastId()
            if err != nil {
                return nil, err
            }
            newId := maxId + 1
            err = txn.Set(key, []byte(strconv.Itoa(newId)))
            if err != nil {
                return nil, err
            }
            err = txn.Set([]byte("max_id"), []byte(strconv.Itoa(newId)))
            if err != nil {
                return nil, err
            }
            current += 2
            if current >= batchSize {
                if err := txn.Commit(); err != nil {
                    return nil, err
                }
                current = 0
                txn = ir.DB.NewTransaction(true)
            }
            sequence = append(sequence, newId)
        } else {
            return nil, err
        }
    }
    
    return sequence, txn.Commit()
}

func (ir *IndexRepository) getLastId() (int, error) {
	var maxId int
    err := ir.DB.View(func(txn *badger.Txn) error {
        item, err := txn.Get([]byte("max_id"))
        if err == badger.ErrKeyNotFound {
            maxId = 1
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