# MATRIX × STUDIO — передача разработчику

Эта ветка `docs/matrix-studio-integration-handoff` отвечает на вопрос: **«Как интегрировать выбранный MATRIX × STUDIO и где сохранять личные рабочие пространства, не меняя оригинальные Чат и Уведомления?»**

- [Согласованное направление UX и backend-контракты](../../docs/ideas/matrix-studio/README.md)
- [Промпт backend-разработчику №005](../../ai/prompts/backend/005-matrix-studio-workspaces/prompt.md)
- [Общая исходная концепция персональных рабочих пространств](../../docs/ideas/pilot-command-terminal/README.ru.md)
- [Production Chat](../../web/features/chat/index.js) и [production Notifications](../../web/features/notifications/index.js): визуальный стиль и функциональность **сохранить**.

**Статус:** только материалы для передачи/ревью. Никакие `web/`, Go, SQL, БД и `main` не изменены данной задачей. Владелец уже получил автономный архив `AstraJourney_MATRIX_STUDIO.zip` в диалоге ChatGPT — это UX-референс на локальных mock данных, **не** готовый код. На этой ветке намеренно нет бинарного ZIP и демо CSS/JS, чтобы не выдавать их за код интеграции; при необходимости скачать архив из исходного диалога.

Требуется отдельная реализационная feature-ветка разработчика и PR на `main`; не merge без решения владельца.
