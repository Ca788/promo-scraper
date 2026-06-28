ALTER TABLE sources DROP CONSTRAINT sources_strategy_check;
ALTER TABLE sources ADD CONSTRAINT sources_strategy_check
    CHECK (strategy IN ('http', 'headless'));
