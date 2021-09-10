/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package txnprocessor

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/trustbloc/edge-core/pkg/log"

	"github.com/trustbloc/sidetree-core-go/pkg/api/operation"
	"github.com/trustbloc/sidetree-core-go/pkg/api/protocol"
	"github.com/trustbloc/sidetree-core-go/pkg/api/txn"
)

var logger = log.New("sidetree-core-observer")

// OperationStore interface to access operation store.
type OperationStore interface {
	Put(ops []*operation.AnchoredOperation) error
}

type unpublishedOperationStore interface {
	// DeleteAll deletes unpublished operation for provided suffixes.
	DeleteAll(suffixes []string) error
}

// Providers contains the providers required by the TxnProcessor.
type Providers struct {
	OpStore                   OperationStore
	OperationProtocolProvider protocol.OperationProvider
}

// TxnProcessor processes Sidetree transactions by persisting them to an operation store.
type TxnProcessor struct {
	*Providers

	unpublishedOperationStore unpublishedOperationStore
	unpublishedOperationTypes []operation.Type
}

// New returns a new document operation processor.
func New(providers *Providers, opts ...Option) *TxnProcessor {
	tp := &TxnProcessor{
		Providers: providers,

		unpublishedOperationStore: &noopUnpublishedOpsStore{},
		unpublishedOperationTypes: []operation.Type{},
	}

	// apply options
	for _, opt := range opts {
		opt(tp)
	}

	return tp
}

// Option is an option for transaction processor.
type Option func(opts *TxnProcessor)

// WithUnpublishedOperationStore is unpublished operation store option.
func WithUnpublishedOperationStore(store unpublishedOperationStore, opTypes []operation.Type) Option {
	return func(opts *TxnProcessor) {
		opts.unpublishedOperationStore = store
		opts.unpublishedOperationTypes = opTypes
	}
}

// Process persists all of the operations for the given anchor.
func (p *TxnProcessor) Process(sidetreeTxn txn.SidetreeTxn, suffixes ...string) error {
	logger.Debugf("processing sidetree txn:%+v, suffixes: %s", sidetreeTxn, suffixes)

	txnOps, err := p.OperationProtocolProvider.GetTxnOperations(&sidetreeTxn)
	if err != nil {
		return fmt.Errorf("failed to retrieve operations for anchor string[%s]: %s", sidetreeTxn.AnchorString, err)
	}

	return p.processTxnOperations(txnOps, sidetreeTxn)
}

func (p *TxnProcessor) processTxnOperations(txnOps []*operation.AnchoredOperation, sidetreeTxn txn.SidetreeTxn) error {
	logger.Debugf("processing %d transaction operations", len(txnOps))

	batchSuffixes := make(map[string]bool)

	var unpublishedOpsSuffixes []string

	var ops []*operation.AnchoredOperation
	for _, op := range txnOps {
		_, ok := batchSuffixes[op.UniqueSuffix]
		if ok {
			logger.Warnf("[%s] duplicate suffix[%s] found in transaction operations: discarding operation %v", sidetreeTxn.Namespace, op.UniqueSuffix, op)

			continue
		}

		updatedOp := updateAnchoredOperation(op, sidetreeTxn)

		logger.Debugf("updated operation with anchoring time: %s", updatedOp.UniqueSuffix)
		ops = append(ops, updatedOp)

		batchSuffixes[op.UniqueSuffix] = true

		if containsOperationType(p.unpublishedOperationTypes, op.Type) {
			unpublishedOpsSuffixes = append(unpublishedOpsSuffixes, op.UniqueSuffix)
		}
	}

	err := p.OpStore.Put(ops)
	if err != nil {
		return errors.Wrapf(err, "failed to store operation from anchor string[%s]", sidetreeTxn.AnchorString)
	}

	err = p.unpublishedOperationStore.DeleteAll(unpublishedOpsSuffixes)
	if err != nil {
		return fmt.Errorf("failed to delete unpublished operations for anchor string[%s]: %w", sidetreeTxn.AnchorString, err)
	}

	return nil
}

func updateAnchoredOperation(op *operation.AnchoredOperation, sidetreeTxn txn.SidetreeTxn) *operation.AnchoredOperation {
	//  The logical anchoring time that this operation was anchored on
	op.TransactionTime = sidetreeTxn.TransactionTime
	// The transaction number of the transaction this operation was batched within
	op.TransactionNumber = sidetreeTxn.TransactionNumber
	// The genesis time of the protocol that was used for this operation
	op.ProtocolGenesisTime = sidetreeTxn.ProtocolGenesisTime

	return op
}

func containsOperationType(values []operation.Type, value operation.Type) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}

	return false
}

type noopUnpublishedOpsStore struct{}

func (noop *noopUnpublishedOpsStore) DeleteAll(_ []string) error {
	return nil
}
