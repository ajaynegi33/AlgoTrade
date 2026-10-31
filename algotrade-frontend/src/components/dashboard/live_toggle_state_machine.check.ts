import assert from "node:assert";

/**
 * Pure state machine representing the ExecutionTopBar mode toggle logic.
 */
interface ExecutionModeState {
  mode: "PAPER" | "LIVE";
  confirmLiveSwitch: boolean;
  isUpdating: boolean;
}

class ExecutionModeStateMachine {
  state: ExecutionModeState;
  updateSettingsCalls: Array<{ mode: "PAPER" | "LIVE" }> = [];

  constructor(initialMode: "PAPER" | "LIVE" = "PAPER") {
    this.state = {
      mode: initialMode,
      confirmLiveSwitch: false,
      isUpdating: false,
    };
  }

  handleToggleLivePrompt() {
    if (this.state.isUpdating || this.state.mode === "LIVE") return;
    this.state.confirmLiveSwitch = !this.state.confirmLiveSwitch;
  }

  handleSelectPaper() {
    if (this.state.isUpdating) return;
    this.state.confirmLiveSwitch = false;
    if (this.state.mode === "LIVE") {
      this.state.mode = "PAPER";
      this.updateSettingsCalls.push({ mode: "PAPER" });
    }
  }

  handleConfirmGoLive() {
    if (this.state.isUpdating) return;
    this.state.confirmLiveSwitch = false;
    this.state.mode = "LIVE";
    this.updateSettingsCalls.push({ mode: "LIVE" });
  }

  handleCancelLive() {
    this.state.confirmLiveSwitch = false;
  }
}

function runExecutionModeChecks() {
  // Test 1: First click on "LIVE ZERODHA" opens confirmation prompt
  const sm = new ExecutionModeStateMachine("PAPER");
  sm.handleToggleLivePrompt();
  assert.strictEqual(sm.state.confirmLiveSwitch, true, "First click must open confirmation");
  assert.strictEqual(sm.state.mode, "PAPER", "Mode must strictly remain PAPER");
  assert.strictEqual(sm.updateSettingsCalls.length, 0, "No API call on toggle click");

  // Test 2: CRITICAL BUG FIX - Second click on "LIVE ZERODHA" closes prompt and NEVER switches to LIVE
  sm.handleToggleLivePrompt();
  assert.strictEqual(sm.state.confirmLiveSwitch, false, "Second click must toggle confirmation closed");
  assert.strictEqual(sm.state.mode, "PAPER", "Mode must still strictly remain PAPER");
  assert.strictEqual(sm.updateSettingsCalls.length, 0, "No API call on second toggle click");

  // Test 3: Third click re-opens confirmation
  sm.handleToggleLivePrompt();
  assert.strictEqual(sm.state.confirmLiveSwitch, true, "Third click re-opens confirmation");

  // Test 4: Clicking "PAPER SIM" while confirmation is open dismisses it safely
  sm.handleSelectPaper();
  assert.strictEqual(sm.state.confirmLiveSwitch, false, "PAPER button dismisses live prompt");
  assert.strictEqual(sm.state.mode, "PAPER");
  assert.strictEqual(sm.updateSettingsCalls.length, 0);

  // Test 5: Clicking Cancel dismisses prompt
  sm.handleToggleLivePrompt();
  assert.strictEqual(sm.state.confirmLiveSwitch, true);
  sm.handleCancelLive();
  assert.strictEqual(sm.state.confirmLiveSwitch, false);
  assert.strictEqual(sm.state.mode, "PAPER");

  // Test 6: ONLY the explicit "YES, SWITCH TO LIVE" action can trigger mode switch to LIVE
  sm.handleToggleLivePrompt();
  assert.strictEqual(sm.state.confirmLiveSwitch, true);
  sm.handleConfirmGoLive();
  assert.strictEqual(sm.state.confirmLiveSwitch, false);
  assert.strictEqual(sm.state.mode, "LIVE", "Explicit confirmation switches to LIVE");
  assert.strictEqual(sm.updateSettingsCalls.length, 1);
  assert.strictEqual(sm.updateSettingsCalls[0].mode, "LIVE");

  // Test 7: When in LIVE mode, clicking "LIVE ZERODHA" is a safe no-op
  sm.handleToggleLivePrompt();
  assert.strictEqual(sm.state.confirmLiveSwitch, false, "Cannot prompt when already live");
  assert.strictEqual(sm.state.mode, "LIVE");

  // Test 8: When in LIVE mode, clicking "PAPER SIM" safely reverts back to PAPER
  sm.handleSelectPaper();
  assert.strictEqual(sm.state.mode, "PAPER");
  assert.strictEqual(sm.updateSettingsCalls.length, 2);
  assert.strictEqual(sm.updateSettingsCalls[1].mode, "PAPER");

  console.log("✓ All 8 Execution Mode & Live Toggle State Machine tests passed!");
}

runExecutionModeChecks();

