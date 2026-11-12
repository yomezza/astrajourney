# Уведомления — сверка с вариантом A

Снимки 12 ноября 2026. Реализация: ветка `feature/notifications-compact-ui`; [отчёт](../../../ai/handoffs/backend/003-notifications-compact-ui.ru.md). В `main` и в выпущенные бинарники эта работа пока не включена.

| Снимок | Происхождение |
|---|---|
| [Эталон A](prototype-a.png) | Автономный прототип из [PR #4](https://github.com/yomezza/astrajourney/pull/4), коммит `b96b5d9cf2fc0a0337d6548a16fd2e3ce1217d3d`; лабораторная сцена остаётся только на эталонном снимке |
| [420 × 540](420x540.png) | Production-модуль в штатном `Modules`, реальные Cosmic API и тестовая PostgreSQL |
| [360 × 420](360x420.png) | Тот же сценарий, уменьшенное окно |
| [320 × 360](320x360.png) | Тот же сценарий, минимальное проверенное окно |
| [Полный текст](detail.png) | Подробности внутри модуля; длинная HTML-подобная строка отображается как текст |

Воспроизведение: `RUN_NOTIFICATIONS_BROWSER=1 go test ./internal/server/integration -run '^TestNotificationsCompactBrowser$' -count=1 -timeout=4m -v`, с отдельной тестовой `TEST_DATABASE_URL`, Node/Playwright и Chromium. Новые снимки сохраняются в игнорируемый `tests/ui/artifacts/notifications/`.

В production не переносились тестовые записи, переключатель A/B/C, лаборатория, собственный оконный менеджер прототипа или фиктивная модель уведомлений.
