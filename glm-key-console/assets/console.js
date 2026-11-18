const issued = document.querySelector('#issued');
if (issued) history.replaceState(null, '', '/ui');
const activation = document.querySelector('#activation-token');
if (activation) activation.value = location.hash.slice(1);
for (const button of document.querySelectorAll('[data-dialog]')) {
  button.addEventListener('click', () => document.getElementById(button.dataset.dialog).showModal());
}
for (const button of document.querySelectorAll('[data-close]')) {
  button.addEventListener('click', () => button.closest('dialog').close());
}
for (const form of document.querySelectorAll('[data-revoke]')) {
  form.addEventListener('submit', event => {
    if (!window.confirm('停用「' + form.dataset.revoke + '」？新的 API 请求将被拒绝。')) event.preventDefault();
  });
}
for (const button of document.querySelectorAll('[data-copy]')) {
  button.addEventListener('click', async () => {
    const field = document.getElementById(button.dataset.copy);
    const status = document.getElementById('toast');
    try {
      await navigator.clipboard.writeText(field.textContent.trim());
      status.textContent = '已复制';
    } catch {
      const range = document.createRange();
      range.selectNodeContents(field);
      window.getSelection().removeAllRanges();
      window.getSelection().addRange(range);
      status.textContent = '内容已选中，请复制';
    }
    status.hidden = false;
    setTimeout(() => { status.hidden = true; }, 2200);
  });
}
