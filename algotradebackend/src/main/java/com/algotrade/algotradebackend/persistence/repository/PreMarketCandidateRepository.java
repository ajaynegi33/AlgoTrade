package com.algotrade.algotradebackend.persistence.repository;

import com.algotrade.algotradebackend.persistence.entity.PreMarketCandidateEntity;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

import org.springframework.transaction.annotation.Transactional;

import java.time.LocalDate;
import java.util.List;

@Repository
public interface PreMarketCandidateRepository extends JpaRepository<PreMarketCandidateEntity, Long> {

    List<PreMarketCandidateEntity> findByScanDateOrderByRankOrderAsc(LocalDate scanDate);

    List<PreMarketCandidateEntity> findByScanDateAndStatusOrderByRankOrderAsc(LocalDate scanDate, String status);

    @Transactional
    long deleteByScanDate(LocalDate scanDate);
}
