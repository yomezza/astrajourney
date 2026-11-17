import { FormState } from "../../../ui/form-state.js";
import { Session, RequestScope } from "../../../core/session.js";
import { scopedView } from "../../../core/scoped-view.js";
import { I18n } from "../../../core/i18n.js";

import { api } from "../../../core/api.js";

import { UI, Modules } from "../../../ui/runtime.js";
import { notify } from "../../../ui/feedback.js";

import { num, fmtTime } from "../../../features/selectors.js";
import { reset } from "../../../ui/view-state.js";

export function createAdministrationBenchmark({ roots, act, command }) {
  const drafts = new FormState(() => Session.data.state?.user.id);
  const n = UI.node,
    tr = (s) => I18n.t(s),
    b = UI.button,
    field = (...args) => drafts.field(UI.field, ...args),
    choice = UI.choice;
  let benchmarkSelected = null;
  const benchmarkScope = new RequestScope(
    () =>
      Session.signedIn &&
      Session.data.state?.user.role === "admin" &&
      Modules.isOpen("benchmark"),
  );
  async function drawBenchmark(lease) {
    const root = roots.benchmark,
      focused =
        root.contains(document.activeElement) &&
        document.activeElement.matches("input");
    const runs = await api("admin/benchmark");
    if (focused || !lease.current()) return false;
    reset(root);
    const fields = n("div", undefined, "settings-fields"),
      players = field(
        fields,
        "Виртуальные игроки",
        "benchmark-players",
        10,
        "number",
      ),
      seconds = field(
        fields,
        "Длительность, секунд",
        "benchmark-seconds",
        10,
        "number",
      ),
      scenario = choice(
        fields,
        "Сценарий",
        [
          ["mixed", "Смешанный"],
          ["quota", "Последняя квота"],
          ["stock", "Последний товар"],
          ["reservations", "Бронирование и получение"],
        ],
        "mixed",
      );
    players.min = 1;
    players.max = 500;
    seconds.min = 5;
    seconds.max = 300;
    root.append(
      fields,
      b(
        "Начать тест",
        () =>
          act("admin/benchmark", {
            action: "start",
            players: Number(players.value),
            seconds: Number(seconds.value),
            scenario: scenario.value,
          }),
        runs.some((r) => r.status === "running"),
      ),
      n(
        "p",
        tr(
          "Виртуальные игроки используют HTTP API и PostgreSQL в одноразовой тестовой схеме той же базы.",
        ),
        "muted",
      ),
    );
    for (const run of runs.filter((r) => r.status === "running")) {
      root.append(
        UI.bar(
          Math.min(
            run.seconds,
            (Date.now() - new Date(run.started_at).getTime()) / 1000,
          ),
          run.seconds,
          "Секунды",
        ),
        n(
          "p",
          num(run.players) +
            " · " +
            num(run.requests) +
            " " +
            tr("Запросы") +
            " · " +
            num(run.errors) +
            " " +
            tr("Ошибки"),
        ),
        b("Остановить тест", () =>
          act("admin/benchmark", { action: "stop", id: run.id }),
        ),
      );
    }
    root.append(
      UI.table(
        [
          "Запуск",
          "Игроки",
          "Сценарий",
          "Статус",
          "Запросы",
          "Ошибки",
          "p95, ms",
        ],
        runs.map((run) => [
          b("#" + run.id, () => {
            benchmarkSelected = run.id;
            renderBenchmark();
          }),
          run.players,
          tr(run.scenario),
          tr(run.status),
          num(run.requests),
          num(run.errors),
          Number(run.metrics.p95_ms || 0).toFixed(1),
        ]),
      ),
    );
    const selected =
      runs.find((r) => r.id === benchmarkSelected) ||
      runs.find((r) => r.status !== "running");
    if (selected) {
      root.append(
        n("h3", tr("Результат") + " #" + selected.id),
        n(
          "p",
          selected.players +
            " · " +
            selected.seconds +
            " s · " +
            tr(selected.scenario) +
            " · v" +
            selected.metrics.version +
            " · " +
            fmtTime(selected.started_at),
          "muted",
        ),
      );
      const previous = runs.find(
        (r) =>
          r.id < selected.id &&
          r.scenario === selected.scenario &&
          r.players === selected.players &&
          r.seconds === selected.seconds,
      );
      root.append(
        UI.table(
          ["Показатель", "Текущий запуск", "Предыдущий запуск"],
          [
            "rps",
            "p50_ms",
            "p95_ms",
            "p99_ms",
            "max_ms",
            "integrity_violations",
            "expected_conflicts",
            "pool_max",
            "db_acquire_ms",
            "db_wait_count",
          ].map((key) => [
            tr(key),
            typeof selected.metrics[key] === "number"
              ? Number(selected.metrics[key]).toFixed(2)
              : "—",
            previous?.metrics[key] !== undefined
              ? Number(previous.metrics[key]).toFixed(2)
              : "—",
          ]),
        ),
      );
      for (const key of ["rps", "p95_ms", "errors"]) {
        const row = n("div", undefined, "benchmark-chart-row");
        row.append(n("span", tr(key)));
        const max = Math.max(
          1,
          ...runs.map((r) =>
            key === "errors" ? r.errors : r.metrics[key] || 0,
          ),
        );
        for (const run of runs.slice(0, 8).reverse()) {
          const bar = n("i"),
            value = key === "errors" ? run.errors : run.metrics[key] || 0;
          bar.style.height = Math.max(2, (value / max) * 65) + "px";
          bar.dataset.helpRaw = "#" + run.id + " · " + value;
          row.append(bar);
        }
        root.append(row);
      }
      if (selected.metrics.failure)
        root.append(n("p", selected.metrics.failure, "error"));
    }
  }
  const renderBenchmark = scopedView(benchmarkScope, drawBenchmark, (error) =>
    notify(error.message, "error"),
  );
  function clear() {
    drafts.clear();
    benchmarkSelected = null;
    benchmarkScope.invalidate();
  }
  window.addEventListener("actionaccepted", () => drafts.clear());
  return {
    renderBenchmark,
    clear,
    close() {
      renderBenchmark.invalidate();
    },
  };
}
