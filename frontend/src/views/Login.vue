<template>
  <div id="login" :class="{ recaptcha: recaptcha }">
    <form @submit="submit">
      <img :src="logoURL" alt="File Browser" />
      <h1>{{ name }}</h1>
      <p v-if="reason != null" class="logout-message">
        {{ t(`login.logout_reasons.${reason}`) }}
      </p>
      <div v-if="error !== ''" class="wrong" role="alert">{{ error }}</div>

      <input
        autofocus
        class="input input--block"
        type="text"
        autocapitalize="off"
        autocomplete="username"
        :aria-label="t('login.username')"
        v-model="username"
        :placeholder="t('login.username')"
      />
      <input
        class="input input--block"
        type="password"
        :aria-label="t('login.password')"
        :autocomplete="createMode ? 'new-password' : 'current-password'"
        v-model="password"
        :placeholder="t('login.password')"
      />
      <input
        class="input input--block"
        v-if="createMode"
        type="password"
        v-model="passwordConfirm"
        :placeholder="t('login.passwordConfirm')"
      />

      <div v-if="recaptcha" id="recaptcha"></div>
      <input
        class="button button--block"
        type="submit"
        :disabled="keyPending"
        :value="createMode ? t('login.signup') : t('login.submit')"
      />
      <button
        v-if="securityKeysEnabled && !createMode"
        class="button button--block button--flat"
        type="button"
        :disabled="keyPending || !canUseKey"
        @click="submitKey"
      >
        {{ t(keyPending ? "securityKeys.waiting" : "securityKeys.signIn") }}
      </button>
      <p v-if="securityKeysEnabled && !canUseKey && !createMode">
        {{ t("securityKeys.unsupported") }}
      </p>

      <p @click="toggleMode" v-if="signup">
        {{ createMode ? t("login.loginInstead") : t("login.createAnAccount") }}
      </p>
    </form>
  </div>
</template>

<script setup lang="ts">
import { StatusError } from "@/api/utils";
import * as auth from "@/utils/auth";
import * as keys from "@/api/securityKeys";
import {
  isSecurityKeyCancellation,
  supportsSecurityKeys,
} from "@/utils/webauthn";
import {
  name,
  logoURL,
  recaptcha,
  recaptchaKey,
  signup,
  securityKeysEnabled,
} from "@/utils/constants";
import { inject, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

// Define refs
const createMode = ref<boolean>(false);
const error = ref<string>("");
const username = ref<string>("");
const password = ref<string>("");
const passwordConfirm = ref<string>("");
const keyPending = ref(false);
const canUseKey = supportsSecurityKeys();

const route = useRoute();
const router = useRouter();
const { t } = useI18n({});
// Define functions
const toggleMode = () => (createMode.value = !createMode.value);

const $showError = inject<IToastError>("$showError")!;

const reason = route.query["logout-reason"] ?? null;

const submitKey = async () => {
  error.value = "";
  if (!username.value) {
    error.value = t("securityKeys.enterUsername");
    return;
  }
  const captcha = recaptcha ? window.grecaptcha?.getResponse() : "";
  if (recaptcha && !captcha) {
    error.value = t("login.wrongCredentials");
    return;
  }
  keyPending.value = true;
  try {
    await keys.login(username.value, captcha ?? "");
    await router.push({ path: (route.query.redirect || "/files/") as string });
  } catch (err) {
    error.value = t(
      isSecurityKeyCancellation(err)
        ? "securityKeys.canceled"
        : "securityKeys.signInFailed"
    );
    if (recaptcha) window.grecaptcha?.reset();
  } finally {
    keyPending.value = false;
  }
};

const submit = async (event: Event) => {
  event.preventDefault();
  event.stopPropagation();
  if (keyPending.value) return;

  const redirect = (route.query.redirect || "/files/") as string;

  let captcha = "";
  if (recaptcha) {
    captcha = window.grecaptcha.getResponse();

    if (captcha === "") {
      error.value = t("login.wrongCredentials");
      return;
    }
  }

  if (createMode.value) {
    if (password.value !== passwordConfirm.value) {
      error.value = t("login.passwordsDontMatch");
      return;
    }
  }

  try {
    if (createMode.value) {
      await auth.signup(username.value, password.value);
    }

    await auth.login(username.value, password.value, captcha);
    router.push({ path: redirect });
  } catch (e: any) {
    // console.error(e);
    if (e instanceof StatusError) {
      if (e.status === 409) {
        error.value = t("login.usernameTaken");
      } else if (e.status === 403) {
        error.value = t("login.wrongCredentials");
      } else if (e.status === 400) {
        const match = e.message.match(/minimum length is (\d+)/);
        if (match) {
          error.value = t("login.passwordTooShort", { min: match[1] });
        } else {
          error.value = e.message;
        }
      } else {
        $showError(e);
      }
    }
  }
};

// Run hooks
onMounted(() => {
  if (!recaptcha) return;

  window.grecaptcha.ready(function () {
    window.grecaptcha.render("recaptcha", {
      sitekey: recaptchaKey,
    });
  });
});
</script>
