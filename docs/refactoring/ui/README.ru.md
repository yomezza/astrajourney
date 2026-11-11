# Архитектура интерфейса Astra Journey

Текущая версия — **0.13.0**, план M1–M14 завершён. Клиент работает на native ES modules; разделены core, общие UI-компоненты и игровые features.

- [Отчёт по изменениям, проверкам и ограничениям 0.13.0](IMPLEMENTATION-0.13.0.ru.md).
- [Действующий каталог механизмов и владельцев](CATALOG.ru.md).
- [Правила разработки](DEVELOPMENT.ru.md).
- [Запуск тестов](../../../tests/ui/README.ru.md).
- [Границы космической и будущей планетарной служб](../service-boundaries.ru.md).
- Проверенные сцены: [планировщик](screenshots/desktop-planner.png), [корабль/трюм](screenshots/desktop-ship-cargo.png).

## Исходный аудит M0

Документы ниже описывают **исторический срез 0.12.5**, commit `71659a96fdb71be9bc154fe87af41664e800003d`. Их pinned source references намеренно не переписаны на новую структуру. Старые имена файлов и TODO в M0-results не означают, что они остаются в текущем runtime.

- [Исходный план M0–M14](MIGRATION.ru.md).
- [Результат и проверки M0](M0-results.ru.md).
- [Компоненты](components.md) / [CSV](components.csv).
- [Настройки, defaults и scope](preferences.md) / [CSV](preferences.csv).
- [Повторяющиеся решения](duplications.md) / [CSV](duplications.csv).
- [Предложенные механизмы](proposed-ui.md) / [CSV](proposed-ui.csv).
- [Указатели исходников аудированного commit](source-references.md).
- Исходные снимки: [планировщик](baseline/desktop-planner.png), [корабль/трюм](baseline/desktop-ship-cargo.png).

M0 добавлял только тестовую инфраструктуру и документы, без игрового EXE. Последующее разрешение пользователя выполнить весь план заменило первоначальное требование останавливаться после каждого этапа. Состояние прежней паузы сохранено в [RESUME.ru.md](../RESUME.ru.md); текущий результат описывает отчёт 0.13.0.
