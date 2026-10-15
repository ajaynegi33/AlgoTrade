package com.algotrade.algotradebackend.persistence.entity;

import jakarta.persistence.*;
import java.time.Instant;

/**
 * JPA Entity representing an active, currently held open paper trade position.
 * Persisted in `algotradepaper.db` so active paper positions survive backend restarts.
 */
@Entity
@Table(name = "paper_active_positions")
public class PaperActivePositionEntity {

    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    private Long id;

    @Column(nullable = false)
    private String symbol;

    @Column(nullable = false)
    private Integer quantity;

    @Column(nullable = false)
    private Double entryPrice;

    @Column(nullable = false)
    private Double currentPrice;

    @Column(nullable = false)
    private Double initialStopLoss;

    @Column(nullable = false)
    private Double activeTrailingStop;

    @Column(nullable = false)
    private Double highWaterMark;

    @Column(nullable = false)
    private Instant entryTime;

    @Column
    private String activeSlOrderId;

    public PaperActivePositionEntity() {}

    public PaperActivePositionEntity(
        String symbol,
        Integer quantity,
        Double entryPrice,
        Double currentPrice,
        Double initialStopLoss,
        Double activeTrailingStop,
        Double highWaterMark,
        Instant entryTime,
        String activeSlOrderId
    ) {
        this.symbol = symbol;
        this.quantity = quantity;
        this.entryPrice = entryPrice;
        this.currentPrice = currentPrice;
        this.initialStopLoss = initialStopLoss;
        this.activeTrailingStop = activeTrailingStop;
        this.highWaterMark = highWaterMark;
        this.entryTime = entryTime;
        this.activeSlOrderId = activeSlOrderId;
    }

    public Long getId() { return id; }
    public String getSymbol() { return symbol; }
    public void setSymbol(String symbol) { this.symbol = symbol; }

    public Integer getQuantity() { return quantity; }
    public void setQuantity(Integer quantity) { this.quantity = quantity; }

    public Double getEntryPrice() { return entryPrice; }
    public void setEntryPrice(Double entryPrice) { this.entryPrice = entryPrice; }

    public Double getCurrentPrice() { return currentPrice; }
    public void setCurrentPrice(Double currentPrice) { this.currentPrice = currentPrice; }

    public Double getInitialStopLoss() { return initialStopLoss; }
    public void setInitialStopLoss(Double initialStopLoss) { this.initialStopLoss = initialStopLoss; }

    public Double getActiveTrailingStop() { return activeTrailingStop; }
    public void setActiveTrailingStop(Double activeTrailingStop) { this.activeTrailingStop = activeTrailingStop; }

    public Double getHighWaterMark() { return highWaterMark; }
    public void setHighWaterMark(Double highWaterMark) { this.highWaterMark = highWaterMark; }

    public Instant getEntryTime() { return entryTime; }
    public void setEntryTime(Instant entryTime) { this.entryTime = entryTime; }

    public String getActiveSlOrderId() { return activeSlOrderId; }
    public void setActiveSlOrderId(String activeSlOrderId) { this.activeSlOrderId = activeSlOrderId; }
}
