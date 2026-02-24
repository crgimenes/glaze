const input = document.getElementById("taskInput");
const addBtn = document.getElementById("addBtn");
const taskList = document.getElementById("taskList");
const empty = document.getElementById("empty");
const count = document.getElementById("count");

function escapeHtml(text) {
  const div = document.createElement("div");
  div.textContent = text;
  return div.innerHTML;
}

function render(items) {
  taskList.innerHTML = "";
  const total = items ? items.length : 0;
  const label = total === 1 ? "item" : "items";
  count.textContent = total + " " + label;

  if (!items || items.length === 0) {
    empty.style.display = "block";
    return;
  }
  empty.style.display = "none";

  for (const item of items) {
    const li = document.createElement("li");
    li.className = "list-group-item";
    if (item.done) {
      li.classList.add("done");
    }

    const left = document.createElement("div");
    left.className = "left";

    const text = document.createElement("span");
    text.className = "text";
    text.innerHTML = escapeHtml(item.text);
    left.appendChild(text);

    const actions = document.createElement("div");
    actions.className = "actions";

    const toggle = document.createElement("button");
    toggle.type = "button";
    toggle.className = item.done ? "btn btn-success" : "btn btn-outline-secondary";
    toggle.textContent = item.done ? "Done" : "Mark";
    toggle.addEventListener("click", async () => {
      const updated = await window.tasks_toggle(item.id);
      render(updated);
    });

    const del = document.createElement("button");
    del.type = "button";
    del.className = "btn btn-outline-danger";
    del.textContent = "Delete";
    del.addEventListener("click", async () => {
      const updated = await window.tasks_delete(item.id);
      render(updated);
    });

    actions.appendChild(toggle);
    actions.appendChild(del);
    li.appendChild(left);
    li.appendChild(actions);
    taskList.appendChild(li);
  }
}

async function addTask() {
  const text = input.value.trim();
  if (!text) return;
  const updated = await window.tasks_add(text);
  input.value = "";
  render(updated);
  input.focus();
}

async function bootstrap() {
  addBtn.addEventListener("click", addTask);
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      addTask();
    }
  });

  const current = await window.tasks_list();
  render(current);
}

bootstrap();
