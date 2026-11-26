<template>
  <errors v-if="error" :errorCode="error.status" />
  <div class="row" v-else-if="!layoutStore.loading">
    <div class="column">
      <form @submit="save" class="card">
        <div class="card-title">
          <h2 v-if="user?.id === 0">{{ $t("settings.newUser") }}</h2>
          <h2 v-else>{{ $t("settings.user") }} {{ user?.username }}</h2>
        </div>

        <div class="card-content" v-if="user">
          <user-form
            v-model:user="user"
            v-model:createUserDir="createUserDir"
            :isDefault="false"
            :isNew="isNew"
            :hide-password="
              securityKeysEnabled && !isNew && (keyActive || !keyStatusKnown)
            "
          />
        </div>

        <div class="card-action">
          <button
            v-if="!isNew"
            @click.prevent="deletePrompt"
            type="button"
            class="button button--flat button--red"
            :aria-label="$t('buttons.delete')"
            :title="$t('buttons.delete')"
          >
            {{ $t("buttons.delete") }}
          </button>
          <router-link to="/settings/users">
            <button
              class="button button--flat button--grey"
              :aria-label="$t('buttons.cancel')"
              :title="$t('buttons.cancel')"
            >
              {{ $t("buttons.cancel") }}
            </button>
          </router-link>
          <input
            class="button button--flat"
            type="submit"
            :value="$t('buttons.save')"
          />
        </div>
      </form>
    </div>
    <div
      v-if="
        securityKeysEnabled && user && !isNew && user.id !== authStore.user?.id
      "
      class="column"
    >
      <SecurityKeys
        :key="user.id"
        :user-id="user.id"
        :is-self="false"
        :lock-password="user.lockPassword"
        @active="keyStateChanged"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { useAuthStore } from "@/stores/auth";
import { useLayoutStore } from "@/stores/layout";
import { users as api, settings } from "@/api";
import UserForm from "@/components/settings/UserForm.vue";
import Errors from "@/views/Errors.vue";
import { computed, inject, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { StatusError } from "@/api/utils";
import { authMethod, securityKeysEnabled } from "@/utils/constants";
import { logout } from "@/utils/auth";
import SecurityKeys from "@/components/settings/SecurityKeys.vue";
import * as keys from "@/api/securityKeys";

const error = ref<StatusError>();
const originalUser = ref<IUser>();
const user = ref<IUser>();
const createUserDir = ref<boolean>(false);
const isCurrentPasswordRequired = ref<boolean>(false);
const keyActive = ref(false);
const keyStatusKnown = ref(false);

const $showError = inject<IToastError>("$showError")!;
const $showSuccess = inject<IToastSuccess>("$showSuccess")!;

const authStore = useAuthStore();
const layoutStore = useLayoutStore();
const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const keyStateChanged = (active: boolean) => {
  keyActive.value = active;
  keyStatusKnown.value = true;
};

onMounted(() => {
  fetchData();
});

const isNew = computed(() => route.path === "/settings/users/new");

watch(route, () => fetchData());

const fetchData = async () => {
  layoutStore.loading = true;
  keyStatusKnown.value = false;

  try {
    if (isNew.value) {
      const { defaults, createUserDir: _createUserDir } = await settings.get();
      isCurrentPasswordRequired.value = authMethod == "json";
      createUserDir.value = _createUserDir;
      user.value = {
        ...defaults,
        username: "",
        password: "",
        rules: [],
        lockPassword: false,
        id: 0,
      };
    } else {
      const { authMethod } = await settings.get();
      isCurrentPasswordRequired.value = authMethod == "json";
      const id = Array.isArray(route.params.id)
        ? route.params.id.join("")
        : route.params.id;
      user.value = { ...(await api.get(parseInt(id))) };
      if (user.value.id === authStore.user?.id)
        keyStateChanged(authStore.user?.keyOnly === true);
    }
  } catch (err) {
    if (err instanceof Error) {
      error.value = err;
    }
  } finally {
    layoutStore.loading = false;
  }
};

const deletePrompt = () => {
  if (authStore.user?.keyOnly) {
    layoutStore.showHover({
      prompt: "deleteUser",
      confirm: () => deleteUser(""),
    });
    return;
  }
  if (isCurrentPasswordRequired.value) {
    layoutStore.showHover({
      prompt: "current-password",
      confirm: (event: Event, currentPassword: string) => {
        event.preventDefault();
        layoutStore.closeHovers();
        deleteUser(currentPassword);
      },
    });
  } else {
    layoutStore.showHover({
      prompt: "deleteUser",
      confirm: () => deleteUser(""),
    });
  }
};

const deleteUser = async (currentPassword: string) => {
  if (!user.value) {
    return false;
  }
  try {
    const proof = authStore.user?.keyOnly
      ? await keys.verify("delete-user", user.value.id)
      : undefined;
    await api.remove(user.value.id, currentPassword, proof);
    if (user.value.id == authStore.user?.id) {
      logout();
    } else {
      router.push({ path: "/settings/users" });
    }
    $showSuccess(t("settings.userDeleted"));
  } catch (err) {
    if (err instanceof StatusError) {
      err.status === 403 ? $showError(t("errors.forbidden")) : $showError(err);
    } else if (err instanceof Error) {
      $showError(err);
    }
  }

  return true;
};

const save = (event: Event) => {
  event.preventDefault();
  if (authStore.user?.keyOnly) {
    void send("");
    return;
  }
  if (isCurrentPasswordRequired.value) {
    layoutStore.showHover({
      prompt: "current-password",
      confirm: (event: Event, currentPassword: string) => {
        event.preventDefault();
        layoutStore.closeHovers();
        send(currentPassword);
      },
    });
  } else {
    send("");
  }

  return true;
};

const send = async (currentPassword: string) => {
  if (!user.value) {
    return false;
  }

  try {
    if (isNew.value) {
      const newUser: IUser = {
        ...originalUser?.value,
        ...user.value,
      };

      const proof = authStore.user?.keyOnly
        ? await keys.verify("create-user", 0)
        : undefined;
      const loc = await api.create(newUser, currentPassword, proof);
      router.push({ path: loc || "/settings/users" });
      $showSuccess(t("settings.userCreated"));
    } else {
      const proof = authStore.user?.keyOnly
        ? await keys.verify("update-user", user.value.id)
        : undefined;
      await api.update(user.value, ["all"], currentPassword, proof);

      if (user.value.id === authStore.user?.id) {
        authStore.updateUser(user.value);
      }

      $showSuccess(t("settings.userUpdated"));
    }
  } catch (e: any) {
    $showError(e);
  }
};
</script>
