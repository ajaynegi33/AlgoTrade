package com.algotrade.algotradebackend.persistence.repository;

import com.algotrade.algotradebackend.persistence.entity.PaperTradeEntity;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

import java.time.Instant;
import java.util.List;

/**
 * Spring Data JPA Repository for querying paper trades stored in `algotradepaper.db`.
 */
@Repository
public interface PaperTradeRepository extends JpaRepository<PaperTradeEntity, Long> {

    /**
     * Finds all paper trades ordered by most recent exit first.
     */
    List<PaperTradeEntity> findAllByOrderByExitTimeDesc();

    /**
     * Finds trades entered within a specific time window (e.g. today's trading session).
     * Used by the risk engine to verify whether the daily trade limit has been reached.
     */
    List<PaperTradeEntity> findByEntryTimeBetween(Instant start, Instant end);

    /**
     * Counts how many trades were entered today.
     */
    long countByEntryTimeBetween(Instant start, Instant end);
}
