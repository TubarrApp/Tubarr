import "jsvectormap/dist/jsvectormap.min.css";
import "flatpickr/dist/flatpickr.min.css";
import "dropzone/dist/dropzone.css";
import "../css/prism.css";
import "../css/style.css";
import "../js/components/prism.js";

import persist from "@alpinejs/persist";
import Alpine from "alpinejs";
import flatpickr from "flatpickr";
import Dropzone from "dropzone";

import chart01 from "./components/charts/chart-01";
import chart02 from "./components/charts/chart-02";
import chart03 from "./components/charts/chart-03";
import chart04 from "./components/charts/chart-04";
import chart05 from "./components/charts/chart-05";
import chart06 from "./components/charts/chart-06";
import chart07 from "./components/charts/chart-07";
import chart08 from "./components/charts/chart-08";
import chart09 from "./components/charts/chart-09";
import chart10 from "./components/charts/chart-10";
import chart11 from "./components/charts/chart-11";
import chart12 from "./components/charts/chart-12";
import chart13 from "./components/charts/chart-13";
import chart14 from "./components/charts/chart-14";
import chart15 from "./components/charts/chart-15";
import chart16 from "./components/charts/chart-16";
import chart17 from "./components/charts/chart-17";
import chart18 from "./components/charts/chart-18";
import chart19 from "./components/charts/chart-19";
import chart20 from "./components/charts/chart-20";
import chart21 from "./components/charts/chart-21";
import chart22 from "./components/charts/chart-22";
import chart23 from "./components/charts/chart-23";
import chart24 from "./components/charts/chart-24";
import map01 from "./components/map-01";
import "./components/carousels/carousel-01";
import "./components/carousels/carousel-02";
import "./components/carousels/carousel-03";
import "./components/carousels/carousel-04";
import "./components/trending-stocks.js";
import "./components/calendar-init.js";
import "./components/task-drag.js";
import "./components/image-resize";

Alpine.plugin(persist);
window.Alpine = Alpine;

// Register Alpine.js components before initializing
Alpine.data("dropdown", () => ({
  open: false,
  toggle() {
    this.open = !this.open;
    if (this.open) this.position();
  },
  position() {
    this.$nextTick(() => {
      const button = this.$el;
      const dropdown = this.$refs.dropdown;
      const rect = button.getBoundingClientRect();

      dropdown.style.position = "fixed";
      dropdown.style.top = `${rect.bottom + window.scrollY}px`;
      dropdown.style.right = `${window.innerWidth - rect.right}px`;
      dropdown.style.zIndex = "999";

      // Reposition if would overflow viewport
      const dropdownRect = dropdown.getBoundingClientRect();
      if (dropdownRect.bottom > window.innerHeight) {
        dropdown.style.top = `${rect.top + window.scrollY - dropdownRect.height}px`;
      }
    });
  },
  init() {
    this.$watch("open", (value) => {
      if (value) this.position();
    });
  },
}));

// Start Alpine.js after registering components
Alpine.start();

// Init flatpickr
flatpickr(".datepicker", {
  mode: "range",
  static: true,
  monthSelectorType: "static",
  dateFormat: "M j, Y",
  defaultDate: [new Date().setDate(new Date().getDate() - 6), new Date()],
  prevArrow:
    '<svg class="stroke-current" width="24" height="24" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M15.25 6L9 12.25L15.25 18.5" stroke="" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  nextArrow:
    '<svg class="stroke-current" width="24" height="24" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M8.75 19L15 12.75L8.75 6.5" stroke="" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  onReady: (selectedDates, dateStr, instance) => {
    // eslint-disable-next-line no-param-reassign
    instance.element.value = dateStr.replace("to", "-");
    const customClass = instance.element.getAttribute("data-class");
    instance.calendarContainer.classList.add(customClass);
  },
  onChange: (selectedDates, dateStr, instance) => {
    // eslint-disable-next-line no-param-reassign
    instance.element.value = dateStr.replace("to", "-");
  },
});
flatpickr(".datepickerTwo", {
  static: true,
  monthSelectorType: "static",
  dateFormat: "M j, Y",
  prevArrow:
    '<svg class="stroke-current" width="24" height="24" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M15.25 6L9 12.25L15.25 18.5" stroke="" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  nextArrow:
    '<svg class="stroke-current" width="24" height="24" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M8.75 19L15 12.75L8.75 6.5" stroke="" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  onReady: (selectedDates, dateStr, instance) => {
    // eslint-disable-next-line no-param-reassign
    instance.element.value = dateStr.replace("to", "-");
    const customClass = instance.element.getAttribute("data-class");
    instance.calendarContainer.classList.add(customClass);
  },
  onChange: (selectedDates, dateStr, instance) => {
    // eslint-disable-next-line no-param-reassign
    instance.element.value = dateStr.replace("to", "-");
  },
});

// Init Dropzone
const dropzoneArea = document.querySelectorAll("#demo-upload");

if (dropzoneArea.length) {
  let myDropzone = new Dropzone("#demo-upload", { url: "/file/post" });
}

// Document Loaded
document.addEventListener("DOMContentLoaded", () => {
  chart01();
  chart02();
  chart03();
  chart04();
  chart05();
  chart06();
  chart07();
  chart08();
  chart09();
  chart10();
  chart11();
  chart12();
  chart13();
  chart14();
  chart15();
  chart16();
  chart17();
  chart18();
  chart19();
  chart20();
  chart21();
  chart22();
  chart23();
  chart24();
  map01();
});

// Get the current year
const year = document.getElementById("year");
if (year) {
  year.textContent = new Date().getFullYear();
}

//Otp
document.addEventListener("DOMContentLoaded", () => {
  const otpInputs = document.querySelectorAll("#otp-container .otp-input");

  otpInputs.forEach((input, index) => {
    input.addEventListener("input", (e) => {
      const value = e.target.value;

      // If a number is entered, move to the next input
      if (value && index < otpInputs.length - 1) {
        otpInputs[index + 1].focus();
      }

      // If the last input is filled, blur the input
      if (index === otpInputs.length - 1 && value) {
        e.target.blur();
      }
    });

    input.addEventListener("keydown", (e) => {
      if (e.key === "Backspace") {
        if (!e.target.value && index > 0) {
          // If backspace is pressed on an empty input, move to the previous input
          otpInputs[index - 1].focus();
        }
      } else if (e.key === "ArrowLeft" && index > 0) {
        // Navigate left
        otpInputs[index - 1].focus();
      } else if (e.key === "ArrowRight" && index < otpInputs.length - 1) {
        // Navigate right
        otpInputs[index + 1].focus();
      }
    });

    input.addEventListener("paste", (e) => {
      e.preventDefault();

      // Paste the OTP value across all inputs
      const pasteData = e.clipboardData
        .getData("text")
        .slice(0, otpInputs.length);
      otpInputs.forEach((input, i) => {
        input.value = pasteData[i] || "";
      });

      // Focus the last filled input
      const lastFilledIndex = pasteData.length - 1;
      if (lastFilledIndex >= 0 && lastFilledIndex < otpInputs.length) {
        otpInputs[lastFilledIndex].focus();
      }
    });
  });
});

// For Copy//

document.addEventListener("DOMContentLoaded", () => {
  const copyInput = document.getElementById("copy-input");
  if (copyInput) {
    // Select the copy button and input field
    const copyButton = document.getElementById("copy-button");
    const copyText = document.getElementById("copy-text");
    const websiteInput = document.getElementById("website-input");

    // Event listener for the copy button
    copyButton.addEventListener("click", () => {
      // Copy the input value to the clipboard
      navigator.clipboard.writeText(websiteInput.value).then(() => {
        // Change the text to "Copied"
        copyText.textContent = "Copied";

        // Reset the text back to "Copy" after 2 seconds
        setTimeout(() => {
          copyText.textContent = "Copy";
        }, 2000);
      });
    });
  }
});

document.addEventListener("DOMContentLoaded", function () {
  const searchInput = document.getElementById("search-input");
  const searchButton = document.getElementById("search-button");

  // Function to focus the search input
  function focusSearchInput() {
    searchInput.focus();
  }

  // Add click event listener to the search button
  searchButton.addEventListener("click", focusSearchInput);

  // Add keyboard event listener for Cmd+K (Mac) or Ctrl+K (Windows/Linux)
  document.addEventListener("keydown", function (event) {
    if ((event.metaKey || event.ctrlKey) && event.key === "k") {
      event.preventDefault(); // Prevent the default browser behavior
      focusSearchInput();
    }
  });

  // Add keyboard event listener for "/" key
  document.addEventListener("keydown", function (event) {
    if (event.key === "/" && document.activeElement !== searchInput) {
      event.preventDefault(); // Prevent the "/" character from being typed
      focusSearchInput();
    }
  });
});
