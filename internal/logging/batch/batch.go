package batch

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/Chichichkin/K8sLoggingAgent/internal/logging"
)

type Processor struct {
	ctx        context.Context
	sender     logging.LogSender
	config     logging.Config
	batch      []logging.LogEntry
	batchMutex sync.Mutex
	batchChan  chan []logging.LogEntry
	stopCtx    context.CancelFunc
	wg         sync.WaitGroup
}

func NewBatchProcessor(ctx context.Context, sender logging.LogSender, config logging.Config) *Processor {
	nCtx, cancel := context.WithCancel(ctx)
	return &Processor{
		sender:    sender,
		config:    config,
		batchChan: make(chan []logging.LogEntry, 100),
		stopCtx:   cancel,
		ctx:       nCtx,
	}
}

func (bp *Processor) AddEntry(entry logging.LogEntry) {
	bp.batchMutex.Lock()
	defer bp.batchMutex.Unlock()

	bp.batch = append(bp.batch, entry)

	if len(bp.batch) >= bp.config.BatchSize {
		bp.flushBatch()
	}
}

func (bp *Processor) Start() {
	bp.wg.Add(2)
	go bp.batchTimer()
	go bp.processBatches()
}

func (bp *Processor) Stop() {
	bp.stopCtx()
	bp.wg.Wait()
}

func (bp *Processor) batchTimer() {
	defer bp.wg.Done()

	ticker := time.NewTicker(bp.config.BatchTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			bp.batchMutex.Lock()
			if len(bp.batch) > 0 {
				bp.flushBatch()
			}
			bp.batchMutex.Unlock()
		case <-bp.ctx.Done():
			return
		}
	}
}

func (bp *Processor) processBatches() {
	defer bp.wg.Done()

	for {
		select {
		case batch := <-bp.batchChan:
			if err := bp.sender.SendBatch(batch); err != nil {
				log.Printf("Failed to send batch: %v", err)
			}
		case <-bp.ctx.Done():
			return
		}
	}
}

func (bp *Processor) flushBatch() {
	if len(bp.batch) == 0 {
		return
	}

	batchToSend := make([]logging.LogEntry, len(bp.batch))
	copy(batchToSend, bp.batch)

	bp.batch = bp.batch[:0]

	select {
	case bp.batchChan <- batchToSend:
		log.Printf("Sent batch of %d entries for processing", len(batchToSend))
	default:
		log.Printf("Batch channel full, dropping batch")
	}
}
