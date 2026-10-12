package com.algotrade.algotradebackend.persistence.entity;

import jakarta.persistence.*;
import java.time.Instant;
import java.time.LocalDate;

/**
 * JPA Entity recording individual candidate telemetry from the 09:12 AM pre-market scan.
 * Stores both shortlisted stocks and rejected stocks with full rejection diagnostic reasons.
 */
@Entity
@Table(name = "pre_market_candidates")
public class PreMarketCandidateEntity {

    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    private Long id;

    @Column(nullable = false)
    private LocalDate scanDate;

    @Column(nullable = false)
    private String symbol;

    @Column(nullable = false)
    private Double previousClose;

    @Column(nullable = false)
    private Double openPrice;

    @Column(nullable = false)
    private Double gapPct;

    @Column(nullable = false)
    private Long preOpenVolume;

    private Double pdh;

    private Double upperCircuit;

    private Double lowerCircuit;

    private Double circuitHeadroomPct;

    private Double rankingScore;

    private Integer rankOrder;

    @Column(nullable = false)
    private String status; // SHORTLISTED, REJECTED, RESERVE

    private String rejectionReason;

    @Column(nullable = false)
    private Instant scannedAt;

    public PreMarketCandidateEntity() {}

    public PreMarketCandidateEntity(
        LocalDate scanDate,
        String symbol,
        Double previousClose,
        Double openPrice,
        Double gapPct,
        Long preOpenVolume,
        Double pdh,
        Double upperCircuit,
        Double lowerCircuit,
        Double circuitHeadroomPct,
        Double rankingScore,
        Integer rankOrder,
        String status,
        String rejectionReason,
        Instant scannedAt
    ) {
        this.scanDate = scanDate;
        this.symbol = symbol;
        this.previousClose = previousClose;
        this.openPrice = openPrice;
        this.gapPct = gapPct;
        this.preOpenVolume = preOpenVolume;
        this.pdh = pdh;
        this.upperCircuit = upperCircuit;
        this.lowerCircuit = lowerCircuit;
        this.circuitHeadroomPct = circuitHeadroomPct;
        this.rankingScore = rankingScore;
        this.rankOrder = rankOrder;
        this.status = status;
        this.rejectionReason = rejectionReason;
        this.scannedAt = scannedAt;
    }

    public Long getId() { return id; }
    public void setId(Long id) { this.id = id; }

    public LocalDate getScanDate() { return scanDate; }
    public void setScanDate(LocalDate scanDate) { this.scanDate = scanDate; }

    public String getSymbol() { return symbol; }
    public void setSymbol(String symbol) { this.symbol = symbol; }

    public Double getPreviousClose() { return previousClose; }
    public void setPreviousClose(Double previousClose) { this.previousClose = previousClose; }

    public Double getOpenPrice() { return openPrice; }
    public void setOpenPrice(Double openPrice) { this.openPrice = openPrice; }

    public Double getGapPct() { return gapPct; }
    public void setGapPct(Double gapPct) { this.gapPct = gapPct; }

    public Long getPreOpenVolume() { return preOpenVolume; }
    public void setPreOpenVolume(Long preOpenVolume) { this.preOpenVolume = preOpenVolume; }

    public Double getPdh() { return pdh; }
    public void setPdh(Double pdh) { this.pdh = pdh; }

    public Double getUpperCircuit() { return upperCircuit; }
    public void setUpperCircuit(Double upperCircuit) { this.upperCircuit = upperCircuit; }

    public Double getLowerCircuit() { return lowerCircuit; }
    public void setLowerCircuit(Double lowerCircuit) { this.lowerCircuit = lowerCircuit; }

    public Double getCircuitHeadroomPct() { return circuitHeadroomPct; }
    public void setCircuitHeadroomPct(Double circuitHeadroomPct) { this.circuitHeadroomPct = circuitHeadroomPct; }

    public Double getRankingScore() { return rankingScore; }
    public void setRankingScore(Double rankingScore) { this.rankingScore = rankingScore; }

    public Integer getRankOrder() { return rankOrder; }
    public void setRankOrder(Integer rankOrder) { this.rankOrder = rankOrder; }

    public String getStatus() { return status; }
    public void setStatus(String status) { this.status = status; }

    public String getRejectionReason() { return rejectionReason; }
    public void setRejectionReason(String rejectionReason) { this.rejectionReason = rejectionReason; }

    public Instant getScannedAt() { return scannedAt; }
    public void setScannedAt(Instant scannedAt) { this.scannedAt = scannedAt; }
}
