package com.algotrade.algotradebackend.persistence.repository;

import com.algotrade.algotradebackend.persistence.entity.LiveTradeEntity;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

import java.time.Instant;
import java.util.List;

/**
 * Spring Data JPA Repository for querying live broker trades stored in SQLite.
 */
@Repository
public interface LiveTradeRepository extends JpaRepository<LiveTradeEntity, Long> {

    List<LiveTradeEntity> findAllByOrderByExitTimeDesc();

    List<LiveTradeEntity> findByEntryTimeBetween(Instant start, Instant end);

    long countByEntryTimeBetween(Instant start, Instant end);
}
