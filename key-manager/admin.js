const byId = id => document.getElementById(id);
const records = [...document.querySelectorAll(".record")];
const manage = byId("manage");
const confirmBox = byId("confirm");
let selected;
let returnTo;
let timer;
function notify(message) {
  const toast = byId("toast");
  toast.textContent = message;
  toast.hidden = false;
  clearTimeout(timer);
  timer = setTimeout(() => toast.hidden = true, 2200);
}
async function copy(button, value) {
  try {
    await navigator.clipboard.writeText(value);
    button.textContent = "已复制";
    setTimeout(() => button.textContent = "复制", 1800);
  } catch {
    const range = document.createRange();
    const row = button.closest(".record");
    if (row) showDetails(row);
    const code = button.closest(".issued")?.querySelector("code") || byId("full");
    range.selectNodeContents(code);
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    notify("请复制已选中的 Key");
  }
}
const unlimitedCheckbox = byId("create-unlimited");
unlimitedCheckbox.onchange = () => {
  unlimitedCheckbox.form.elements.limit.disabled = unlimitedCheckbox.checked;
};
function showDetails(row) {
  selected = row;
  const data = row.dataset;
  byId("manage-title").textContent = data.label;
  byId("credential").hidden = !data.secret;
  byId("full").textContent = data.secret;
  const unlimited = data.unlimited === "true";
  byId("usage").textContent = data.used + " / " + (unlimited ? "不限额度" : data.limit + " Token");
  byId("rate").textContent = data.rpm + " / " + data.parallel;
  const form = byId("add-form");
  form.reset();
  form.elements.id.value = data.id;
  form.elements.namedItem("action").value = unlimited ? "limited" : "add";
  byId("quota-label").textContent = unlimited ? "设置剩余额度（Token）" : "追加额度（Token）";
  byId("quota-submit").textContent = unlimited ? "启用限额" : "追加额度";
  const unlimitedForm = byId("unlimited-form");
  unlimitedForm.elements.id.value = data.id;
  unlimitedForm.hidden = unlimited || data.status === "已停用";
  form.hidden = data.status === "已停用";
  byId("revoke").hidden = form.hidden;
  if (!manage.open) manage.showModal();
}
document.querySelectorAll("[data-open]").forEach(b => b.onclick = () => byId(b.dataset.open).showModal());
document.querySelectorAll("[data-close]").forEach(b => b.onclick = () => b.closest("dialog").close());
document.querySelectorAll("[data-copy]").forEach(b => b.onclick = () => copy(b, b.dataset.copy));
document.querySelectorAll("[data-manage]").forEach(b => b.onclick = () => showDetails(b.closest(".record")));
byId("copy-full").onclick = e => copy(e.currentTarget, selected.dataset.secret);
document.querySelectorAll("[data-confirm]").forEach(b => b.onclick = () => {
  returnTo = b;
  const action = b.dataset.confirm;
  const verb = action === "delete" ? "删除" : "停用";
  byId("confirm-title").textContent = verb + "「" + selected.dataset.label + "」？";
  byId("consequence").textContent = action === "delete" ?
    "删除后，新请求将被拒绝。这把 Key 和它的用量记录将永久移除。" :
    "停用后，新请求将被拒绝。管理页会保留这把 Key 和它的用量记录。";
  byId("confirm-action").textContent = "确认" + verb;
  const form = confirmBox.querySelector("form");
  form.elements.namedItem("id").value = selected.dataset.id;
  form.elements.namedItem("action").value = action;
  confirmBox.showModal();
});
byId("back").onclick = () => confirmBox.close();
confirmBox.addEventListener("close", () => returnTo?.focus());
const search = byId("search");
const statusFilter = byId("status");
function filter() {
  const term = search.value.trim().toLowerCase();
  let visible = 0;
  for (const row of records) {
    const d = row.dataset;
    row.hidden = !(d.label+" "+d.id+" "+d.secret).toLowerCase().includes(term) ||
      !!statusFilter.value && statusFilter.value !== d.status;
    if (!row.hidden) visible++;
  }
  byId("no-match").hidden = visible > 0;
}
if (search) {
  search.oninput = filter;
  statusFilter.onchange = filter;
  byId("reset").onclick = () => { search.value = ""; statusFilter.value = ""; filter(); search.focus(); };
}
if (location.search) history.replaceState(null, "", location.pathname);
byId("copy-guide").onclick = async () => {
  const source = byId("guide-content");
  const outer = byId("guide-details");
  const wasOpen = outer.open;
  outer.open = true;
  const text = "模型接入公告\n\n" + source.innerText.trim();
  try {
    await navigator.clipboard.writeText(text);
    outer.open = wasOpen;
    notify("公告已复制");
  } catch {
    const range = document.createRange();
    range.selectNodeContents(source);
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    notify("请复制已选中的公告");
  }
};
