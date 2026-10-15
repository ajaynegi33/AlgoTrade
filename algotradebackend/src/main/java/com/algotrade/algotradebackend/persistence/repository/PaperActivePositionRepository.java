package com.algotrade.algotradebackend.persistence.repository;

import com.algotrade.algotradebackend.persistence.entity.PaperActivePositionEntity;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

import java.util.Optional;

@Repository
public interface PaperActivePositionRepository extends JpaRepository<PaperActivePositionEntity, Long> {
    Optional<PaperActivePositionEntity> findFirstByOrderByEntryTimeDesc();
    void deleteBySymbol(String symbol);
}
