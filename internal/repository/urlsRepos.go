package repository

import (
	"fmt"

	"github.com/dgraph-io/badger/v3"
)

const urlsKey = "hashKey:%s"

func (ir *IndexRepository) IndexUrlsByHash(hash [32]byte, urlsStruct []byte) error {
	ir.mu.Lock()
	defer ir.mu.Unlock()
	return ir.DB.Update(func(txn *badger.Txn) error {
		return txn.Set(fmt.Appendf(nil, urlsKey, hash), urlsStruct)
	})
}

func (ir *IndexRepository) GetPageUrlsByHash(hash [32]byte) ([]byte, error) {
	urlsStruct := []byte{}
	ir.mu.Lock()
	defer ir.mu.Unlock()
	return urlsStruct, ir.DB.View(func(txn *badger.Txn) error {
		it, err := txn.Get(fmt.Appendf(nil, urlsKey, hash))
		if err != nil {
			return err
		}
		urlsStruct, err = it.ValueCopy(nil)
		return err
	})
}