package com.algotrade.algotradebackend.persistence.repository;

import com.algotrade.algotradebackend.persistence.entity.MarketUniverseEntity;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;
import org.springframework.transaction.annotation.Transactional;

import java.util.List;
import java.util.Optional;

@Repository
public interface MarketUniverseRepository extends JpaRepository<MarketUniverseEntity, Long> {

    List<MarketUniverseEntity> findByIsFnoTrueOrderBySymbolAsc();

    List<MarketUniverseEntity> findAllByOrderBySymbolAsc();

    Optional<MarketUniverseEntity> findBySymbol(String symbol);

    boolean existsBySymbolAndIsFnoTrue(String symbol);

    long countByIsFnoTrue();

    @Transactional
    long deleteAllByExchange(String exchange);
}
